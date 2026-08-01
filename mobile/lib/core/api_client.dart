import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import 'app_exception.dart';
import 'models/models.dart';
import 'secure_storage.dart';
import 'pinned_client.dart';
import 'server_url.dart';

/// Default control-plane base URL, used only until the user points the app
/// at their own self-hosted backend from Settings. Matches the example
/// server in api/openapi.yaml.
/// Empty on purpose. There is no address that is right for a self-hosted
/// control plane, and a plausible-looking placeholder is worse than none: it
/// makes the first sign-in fail against a domain that does not exist, which
/// reads as the password being wrong rather than the address being unset.
const String kDefaultServerUrl = '';

/// Typed REST client for the NexusVPN control plane, mirroring every
/// endpoint documented in `api/openapi.yaml`. Handles bearer-token auth,
/// transparent refresh-on-401, and self-hosted base URLs.
class ApiClient {
  ApiClient({
    SecureStorage? storage,
    http.Client? httpClient,
    String? baseUrl,
  })  : _storage = storage ?? SecureStorage.instance,
        _http = httpClient ?? http.Client(),
        _baseUrl = baseUrl;

  final SecureStorage _storage;
  http.Client _http;
  String? _baseUrl;

  /// Completes once a prior in-flight refresh finishes, so concurrent 401s
  /// don't each try to refresh the token independently.
  Future<TokenPair?>? _refreshInFlight;

  /// Called whenever a refresh attempt fails outright (refresh token is
  /// itself invalid/expired) so the app can drop the user back to login.
  void Function()? onSessionExpired;

  Future<String> get baseUrl async {
    if (_baseUrl != null && _baseUrl!.isNotEmpty) return _baseUrl!;
    final stored = await _storage.readServerUrl();
    _baseUrl = (stored != null && stored.isNotEmpty) ? stored : kDefaultServerUrl;
    return _baseUrl!;
  }

  /// Reinstalls the stored pin, so a relaunched app is as particular about
  /// which machine it talks to as the one that paired.
  ///
  /// Without this, every restart would fall back to ordinary verification,
  /// which for a self-signed certificate means refusing to connect at all —
  /// so the failure would at least be loud. It is called at startup anyway,
  /// because relying on that would be relying on a bug.
  Future<void> restorePin() async {
    final stored = await _storage.readServerFingerprint();
    if (stored != null && stored.isNotEmpty) {
      _http = PinnedHttpClient.create(stored);
    }
  }

  /// Installs the certificate this client will accept, and nothing else.
  ///
  /// The transport is rebuilt rather than reconfigured, because the pin is
  /// fixed when the connection is made and an existing client would keep
  /// using the old one for connections it had already opened.
  Future<void> setServerFingerprint(String fingerprint) async {
    _http = PinnedHttpClient.create(fingerprint);
    await _storage.saveServerFingerprint(fingerprint);
  }

  Future<void> setBaseUrl(String url) async {
    final normalized = _normalizeBaseUrl(url);
    _baseUrl = normalized;
    await _storage.saveServerUrl(normalized);
  }

  /// Validates as well as tidies. This is the single point where a server
  /// address enters the app, so it is the place the cleartext rule in
  /// server_url.dart can actually be enforced.
  static String _normalizeBaseUrl(String url) => validateServerUrl(url);

  Future<Uri> _uri(String path, [Map<String, dynamic>? query]) async {
    final base = await baseUrl;
    final qp = <String, String>{};
    query?.forEach((k, v) {
      if (v != null) qp[k] = v.toString();
    });
    return Uri.parse('$base$path').replace(
      queryParameters: qp.isEmpty ? null : qp,
    );
  }

  // ---------------------------------------------------------------------
  // Low-level request plumbing
  // ---------------------------------------------------------------------

  Future<Map<String, String>> _headers({bool auth = true}) async {
    final headers = <String, String>{'Content-Type': 'application/json'};
    if (auth) {
      final token = await _storage.readAccessToken();
      if (token != null && token.isNotEmpty) {
        headers['Authorization'] = 'Bearer $token';
      }
    }
    return headers;
  }

  Future<dynamic> _request(
    String method,
    String path, {
    Map<String, dynamic>? query,
    Object? body,
    bool auth = true,
    bool retryOn401 = true,
  }) async {
    final uri = await _uri(path, query);
    final headers = await _headers(auth: auth);
    http.Response resp;
    try {
      resp = await _send(method, uri, headers, body)
          .timeout(const Duration(seconds: 20));
    } on TimeoutException {
      throw ApiException.timeout();
    } catch (e) {
      throw ApiException.network(e, host: uri.host);
    }

    if (resp.statusCode == 401 && auth && retryOn401) {
      final refreshed = await _tryRefresh();
      if (refreshed != null && refreshed.accessToken != null) {
        final retryHeaders = await _headers(auth: auth);
        try {
          resp = await _send(method, uri, retryHeaders, body)
              .timeout(const Duration(seconds: 20));
        } on TimeoutException {
          throw ApiException.timeout();
        } catch (e) {
          throw ApiException.network(e, host: uri.host);
        }
      } else {
        onSessionExpired?.call();
      }
    }

    return _decode(resp);
  }

  Future<http.Response> _send(
    String method,
    Uri uri,
    Map<String, String> headers,
    Object? body,
  ) {
    final encoded = body != null ? jsonEncode(body) : null;
    switch (method) {
      case 'GET':
        return _http.get(uri, headers: headers);
      case 'POST':
        return _http.post(uri, headers: headers, body: encoded);
      case 'PATCH':
        return _http.patch(uri, headers: headers, body: encoded);
      case 'DELETE':
        return _http.delete(uri, headers: headers, body: encoded);
      default:
        throw ArgumentError('Unsupported method $method');
    }
  }

  dynamic _decode(http.Response resp) {
    final status = resp.statusCode;
    dynamic parsed;
    if (resp.body.isNotEmpty) {
      try {
        parsed = jsonDecode(resp.body);
      } catch (_) {
        parsed = null;
      }
    }

    if (status >= 200 && status < 300) {
      return parsed;
    }

    String code = 'http_$status';
    String message = 'Request failed with status $status';
    if (parsed is Map<String, dynamic> && parsed['error'] is Map) {
      final err = parsed['error'] as Map;
      code = (err['code'] as String?) ?? code;
      message = (err['message'] as String?) ?? message;
    } else if (status == 401) {
      message = 'Invalid credentials or session expired.';
    } else if (status == 409) {
      message = 'That already exists.';
    } else if (status == 404) {
      message = 'Not found.';
    }
    throw ApiException(statusCode: status, code: code, message: message);
  }

  /// Attempts a token refresh, de-duplicating concurrent callers.
  Future<TokenPair?> _tryRefresh() {
    final inFlight = _refreshInFlight;
    if (inFlight != null) return inFlight;

    final future = _doRefresh();
    _refreshInFlight = future;
    future.whenComplete(() => _refreshInFlight = null);
    return future;
  }

  Future<TokenPair?> _doRefresh() async {
    final refreshToken = await _storage.readRefreshToken();
    if (refreshToken == null || refreshToken.isEmpty) return null;
    try {
      final uri = await _uri('/auth/refresh');
      final resp = await _http
          .post(
            uri,
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'refreshToken': refreshToken}),
          )
          .timeout(const Duration(seconds: 20));
      if (resp.statusCode != 200) {
        await _storage.clearTokens();
        return null;
      }
      final json = jsonDecode(resp.body) as Map<String, dynamic>;
      final pair = TokenPair.fromJson(json);
      if (pair.accessToken != null && pair.refreshToken != null) {
        await _storage.saveTokens(
          accessToken: pair.accessToken!,
          refreshToken: pair.refreshToken!,
        );
      }
      return pair;
    } catch (_) {
      return null;
    }
  }

  // ---------------------------------------------------------------------
  // auth
  // ---------------------------------------------------------------------

  Future<User> register({
    required String email,
    required String password,
    required String displayName,
  }) async {
    final json = await _request(
      'POST',
      '/auth/register',
      auth: false,
      body: {
        'email': email,
        'password': password,
        'displayName': displayName,
      },
    );
    return User.fromJson(json as Map<String, dynamic>);
  }

  Future<TokenPair> login({
    required String email,
    required String password,
    String? mfaCode,
  }) async {
    final json = await _request(
      'POST',
      '/auth/login',
      auth: false,
      body: {
        'email': email,
        'password': password,
        if (mfaCode != null && mfaCode.isNotEmpty) 'mfaCode': mfaCode,
      },
    );
    return TokenPair.fromJson(json as Map<String, dynamic>);
  }

  /// Spends a pairing code and adopts the session it returns.
  ///
  /// Unauthenticated by necessity: the device calling this has no credentials
  /// yet, which is the entire point of it.
  Future<ClaimedPairing> claimPairing(String token) async {
    final json = await _request(
      'POST',
      '/auth/pair/claim',
      auth: false,
      body: {'token': token},
    );
    return ClaimedPairing.fromJson(json as Map<String, dynamic>);
  }

  Future<TokenPair> refresh(String refreshToken) async {
    final json = await _request(
      'POST',
      '/auth/refresh',
      auth: false,
      retryOn401: false,
      body: {'refreshToken': refreshToken},
    );
    return TokenPair.fromJson(json as Map<String, dynamic>);
  }

  Future<void> logout() async {
    try {
      await _request('POST', '/auth/logout', retryOn401: false);
    } on ApiException {
      // Best-effort: even if the server call fails, local session should
      // still be cleared by the caller.
    }
  }

  Future<Map<String, String>> mfaEnable() async {
    final json =
        await _request('POST', '/auth/mfa/enable') as Map<String, dynamic>;
    return {
      'secret': (json['secret'] as String?) ?? '',
      'otpauthUrl': (json['otpauthUrl'] as String?) ?? '',
    };
  }

  Future<void> mfaVerify(String code) async {
    await _request('POST', '/auth/mfa/verify', body: {'code': code});
  }

  Future<void> passwordForgot(String email) async {
    await _request(
      'POST',
      '/auth/password/forgot',
      auth: false,
      body: {'email': email},
    );
  }

  Future<void> passwordReset({
    required String token,
    required String newPassword,
  }) async {
    await _request(
      'POST',
      '/auth/password/reset',
      auth: false,
      body: {'token': token, 'newPassword': newPassword},
    );
  }

  // ---------------------------------------------------------------------
  // networks
  // ---------------------------------------------------------------------

  Future<List<Network>> listNetworks() async {
    final json = await _request('GET', '/networks') as List<dynamic>;
    return json
        .map((e) => Network.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<Network> createNetwork({
    required String name,
    String? description,
    required String cidr,
    List<String>? dnsServers,
  }) async {
    final json = await _request(
      'POST',
      '/networks',
      body: {
        'name': name,
        'description': ?description,
        'cidr': cidr,
        'dnsServers': ?dnsServers,
      },
    );
    return Network.fromJson(json as Map<String, dynamic>);
  }

  Future<Network> joinNetwork(String inviteCode) async {
    final json = await _request(
      'POST',
      '/networks/join',
      body: {'inviteCode': inviteCode},
    );
    return Network.fromJson(json as Map<String, dynamic>);
  }

  Future<Network> getNetwork(String networkId) async {
    final json = await _request('GET', '/networks/$networkId');
    return Network.fromJson(json as Map<String, dynamic>);
  }

  Future<void> updateNetwork(
    String networkId, {
    String? name,
    String? description,
    String? cidr,
    List<String>? dnsServers,
  }) async {
    await _request(
      'PATCH',
      '/networks/$networkId',
      body: {
        'name': ?name,
        'description': ?description,
        'cidr': ?cidr,
        'dnsServers': ?dnsServers,
      },
    );
  }

  Future<void> deleteNetwork(String networkId) async {
    await _request('DELETE', '/networks/$networkId');
  }

  Future<Map<String, dynamic>> rotateInviteCode(String networkId) async {
    final json =
        await _request('POST', '/networks/$networkId/invite')
            as Map<String, dynamic>;
    return json;
  }

  Future<List<Member>> listMembers(String networkId) async {
    final json =
        await _request('GET', '/networks/$networkId/members') as List<dynamic>;
    return json
        .map((e) => Member.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<void> updateMemberRole(
    String networkId,
    String userId,
    String role,
  ) async {
    await _request(
      'PATCH',
      '/networks/$networkId/members/$userId',
      body: {'role': role},
    );
  }

  Future<void> removeMember(String networkId, String userId) async {
    await _request('DELETE', '/networks/$networkId/members/$userId');
  }

  // ---------------------------------------------------------------------
  // devices
  // ---------------------------------------------------------------------

  Future<List<Device>> listNetworkDevices(String networkId) async {
    final json =
        await _request('GET', '/networks/$networkId/devices') as List<dynamic>;
    return json
        .map((e) => Device.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<Device> registerDevice(
    String networkId, {
    required String name,
    required String os,
    String? osVersion,
    required String publicKey,
  }) async {
    final json = await _request(
      'POST',
      '/networks/$networkId/devices',
      body: {
        'name': name,
        'os': os,
        'osVersion': ?osVersion,
        'publicKey': publicKey,
      },
    );
    return Device.fromJson(json as Map<String, dynamic>);
  }

  Future<Device> getDevice(String deviceId) async {
    final json = await _request('GET', '/devices/$deviceId');
    return Device.fromJson(json as Map<String, dynamic>);
  }

  Future<void> deleteDevice(String deviceId) async {
    await _request('DELETE', '/devices/$deviceId');
  }

  Future<void> heartbeat(String deviceId) async {
    await _request('POST', '/devices/$deviceId/heartbeat');
  }

  Future<List<Device>> getPeers(String deviceId) async {
    final json =
        await _request('GET', '/devices/$deviceId/peers') as List<dynamic>;
    return json
        .map((e) => Device.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  // ---------------------------------------------------------------------
  // logs
  // ---------------------------------------------------------------------

  Future<List<AuditLog>> auditLogs({String? networkId}) async {
    final json = await _request(
      'GET',
      '/logs/audit',
      query: {'networkId': ?networkId},
    ) as List<dynamic>;
    return json
        .map((e) => AuditLog.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<List<ConnectionLog>> connectionLogs({String? networkId}) async {
    final json = await _request(
      'GET',
      '/logs/connections',
      query: {'networkId': ?networkId},
    ) as List<dynamic>;
    return json
        .map((e) => ConnectionLog.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  // ---------------------------------------------------------------------
  // dashboard
  // ---------------------------------------------------------------------

  Future<DashboardStats> dashboardStats() async {
    final json = await _request('GET', '/dashboard/stats');
    return DashboardStats.fromJson(json as Map<String, dynamic>);
  }

  void dispose() {
    _http.close();
  }
}
