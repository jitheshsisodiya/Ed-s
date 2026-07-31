import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nexusvpn/core/api_client.dart';
import 'package:nexusvpn/core/app_exception.dart';

/// Seeds the secure store so ApiClient sees a signed-in session.
void seedSession({String access = 'access-1', String refresh = 'refresh-1'}) {
  FlutterSecureStorage.setMockInitialValues({
    'nexusvpn.access_token': access,
    'nexusvpn.refresh_token': refresh,
    'nexusvpn.server_url': 'https://api.example.com/api/v1',
  });
}

http.Response jsonResponse(Object body, {int status = 200}) =>
    http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  test('login posts credentials and returns the token pair', () async {
    seedSession();
    late http.Request captured;

    final client = ApiClient(
      baseUrl: 'https://api.example.com/api/v1',
      httpClient: MockClient((req) async {
        captured = req;
        return jsonResponse({
          'accessToken': 'a-1',
          'refreshToken': 'r-1',
          'expiresIn': 900,
        });
      }),
    );

    final tokens = await client.login(email: 'alice@example.com', password: 'correct-horse-battery');

    expect(tokens.accessToken, 'a-1');
    expect(captured.method, 'POST');
    expect(captured.url.path, '/api/v1/auth/login');
    expect(jsonDecode(captured.body)['email'], 'alice@example.com');
  });

  test('login surfaces the MFA challenge rather than treating it as success', () async {
    final client = ApiClient(
      baseUrl: 'https://api.example.com/api/v1',
      httpClient: MockClient((_) async => jsonResponse({'mfaRequired': true})),
    );

    final tokens = await client.login(email: 'alice@example.com', password: 'pw');

    expect(tokens.mfaRequired, isTrue);
    expect(tokens.hasTokens, isFalse);
  });

  test('attaches the bearer token to authenticated requests', () async {
    seedSession(access: 'the-access-token');
    String? authHeader;

    final client = ApiClient(
      baseUrl: 'https://api.example.com/api/v1',
      httpClient: MockClient((req) async {
        authHeader = req.headers['authorization'];
        return jsonResponse([]);
      }),
    );

    await client.listNetworks();

    expect(authHeader, 'Bearer the-access-token');
  });

  test('refreshes once on 401 and retries the original request', () async {
    seedSession(access: 'stale-token');
    var networkCalls = 0;
    var refreshCalls = 0;

    final client = ApiClient(
      baseUrl: 'https://api.example.com/api/v1',
      httpClient: MockClient((req) async {
        if (req.url.path.endsWith('/auth/refresh')) {
          refreshCalls++;
          return jsonResponse({
            'accessToken': 'fresh-token',
            'refreshToken': 'r-2',
            'expiresIn': 900,
          });
        }
        networkCalls++;
        if (req.headers['authorization'] == 'Bearer fresh-token') {
          return jsonResponse([
            {'id': 'n-1', 'name': 'Home Lab', 'cidr': '10.77.0.0/24'},
          ]);
        }
        return http.Response('unauthorized', 401);
      }),
    );

    final networks = await client.listNetworks();

    expect(networks, hasLength(1));
    expect(networks.first.name, 'Home Lab');
    expect(refreshCalls, 1, reason: 'exactly one refresh should be attempted');
    expect(networkCalls, 2, reason: 'the original request should be retried once');
  });

  test('signals session expiry when the refresh token is also rejected', () async {
    seedSession(access: 'stale-token');
    var sessionExpired = false;

    final client = ApiClient(
      baseUrl: 'https://api.example.com/api/v1',
      httpClient: MockClient((_) async => http.Response('unauthorized', 401)),
    )..onSessionExpired = () => sessionExpired = true;

    await expectLater(client.listNetworks(), throwsA(isA<ApiException>()));
    expect(sessionExpired, isTrue);
  });

  test('surfaces the API error envelope', () async {
    seedSession();

    final client = ApiClient(
      baseUrl: 'https://api.example.com/api/v1',
      httpClient: MockClient((_) async => jsonResponse(
            {
              'error': {'code': 'not_found', 'message': 'Network not found'}
            },
            status: 404,
          )),
    );

    try {
      await client.getNetwork('missing');
      fail('expected an ApiException');
    } on ApiException catch (e) {
      expect(e.statusCode, 404);
      expect(e.message, contains('Network not found'));
    }
  });

  test('joins a network by invite code', () async {
    seedSession();
    late http.Request captured;

    final client = ApiClient(
      baseUrl: 'https://api.example.com/api/v1',
      httpClient: MockClient((req) async {
        captured = req;
        return jsonResponse({'id': 'n-9', 'name': 'Team', 'cidr': '10.80.0.0/24'});
      }),
    );

    final network = await client.joinNetwork('INVITE-CODE');

    expect(network.id, 'n-9');
    expect(captured.url.path, '/api/v1/networks/join');
    expect(jsonDecode(captured.body)['inviteCode'], 'INVITE-CODE');
  });
}
