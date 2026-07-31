import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Wraps [FlutterSecureStorage] with the specific keys NexusVPN needs:
/// auth tokens, the self-hosted server base URL, and this device's
/// WireGuard keypair (generated on-device, private key never leaves it).
class SecureStorage {
  SecureStorage._();

  static final SecureStorage instance = SecureStorage._();

  // Android: the plugin encrypts with its own ciphers backed by the
  // Keystore. (The old encryptedSharedPreferences option is deprecated and
  // ignored — Google deprecated the Jetpack Security library it used.)
  // iOS: first_unlock keeps the keychain item readable by the VPN extension
  // after a reboot, while still requiring the device to have been unlocked
  // once, and it is never synchronised to iCloud.
  static const _storage = FlutterSecureStorage(
    iOptions: IOSOptions(accessibility: KeychainAccessibility.first_unlock),
  );

  static const _kAccessToken = 'nexusvpn.access_token';
  static const _kRefreshToken = 'nexusvpn.refresh_token';
  static const _kServerUrl = 'nexusvpn.server_url';
  static const _kDevicePrivateKey = 'nexusvpn.device_private_key';
  static const _kDevicePublicKey = 'nexusvpn.device_public_key';
  static const _kDeviceId = 'nexusvpn.device_id';
  static const _kLastUserEmail = 'nexusvpn.last_user_email';

  Future<void> saveTokens({
    required String accessToken,
    required String refreshToken,
  }) async {
    await Future.wait([
      _storage.write(key: _kAccessToken, value: accessToken),
      _storage.write(key: _kRefreshToken, value: refreshToken),
    ]);
  }

  Future<String?> readAccessToken() => _storage.read(key: _kAccessToken);

  Future<String?> readRefreshToken() => _storage.read(key: _kRefreshToken);

  Future<void> clearTokens() async {
    await Future.wait([
      _storage.delete(key: _kAccessToken),
      _storage.delete(key: _kRefreshToken),
    ]);
  }

  Future<void> saveServerUrl(String url) =>
      _storage.write(key: _kServerUrl, value: url);

  Future<String?> readServerUrl() => _storage.read(key: _kServerUrl);

  Future<void> saveDeviceKeypair({
    required String privateKey,
    required String publicKey,
  }) async {
    await Future.wait([
      _storage.write(key: _kDevicePrivateKey, value: privateKey),
      _storage.write(key: _kDevicePublicKey, value: publicKey),
    ]);
  }

  Future<String?> readDevicePrivateKey() =>
      _storage.read(key: _kDevicePrivateKey);

  Future<String?> readDevicePublicKey() =>
      _storage.read(key: _kDevicePublicKey);

  Future<void> saveDeviceId(String id) =>
      _storage.write(key: _kDeviceId, value: id);

  Future<String?> readDeviceId() => _storage.read(key: _kDeviceId);

  Future<void> saveLastUserEmail(String email) =>
      _storage.write(key: _kLastUserEmail, value: email);

  Future<String?> readLastUserEmail() =>
      _storage.read(key: _kLastUserEmail);

  /// Clears everything except the server URL and remembered e-mail, which
  /// are convenience fields the user typically wants preserved across a
  /// logout on the same self-hosted server.
  Future<void> clearSession() async {
    await Future.wait([
      _storage.delete(key: _kAccessToken),
      _storage.delete(key: _kRefreshToken),
      _storage.delete(key: _kDevicePrivateKey),
      _storage.delete(key: _kDevicePublicKey),
      _storage.delete(key: _kDeviceId),
    ]);
  }

  Future<void> wipeAll() => _storage.deleteAll();
}
