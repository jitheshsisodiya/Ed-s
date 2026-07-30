import 'package:flutter/foundation.dart';

import 'api_client.dart';
import 'app_exception.dart';
import 'models/models.dart';
import 'secure_storage.dart';

enum AuthStatus {
  /// Bootstrap hasn't finished checking storage yet - show a splash screen.
  unknown,
  authenticated,
  unauthenticated,

  /// Login succeeded on the credentials check but the account requires a
  /// TOTP code before tokens are issued.
  mfaRequired,
}

/// Session/auth state for the whole app. Owns the [ApiClient] instance so
/// screens reach the API exclusively through `context.read<AuthProvider>().api`.
class AuthProvider extends ChangeNotifier {
  AuthProvider({ApiClient? apiClient, SecureStorage? storage})
      : api = apiClient ?? ApiClient(),
        _storage = storage ?? SecureStorage.instance {
    api.onSessionExpired = _handleSessionExpired;
  }

  final ApiClient api;
  final SecureStorage _storage;

  AuthStatus status = AuthStatus.unknown;
  String? errorMessage;
  bool busy = false;
  String? lastKnownEmail;

  // Held only in memory between the first login attempt (mfaRequired=true)
  // and the follow-up submission that includes the TOTP code.
  String? _pendingEmail;
  String? _pendingPassword;

  bool get isAuthenticated => status == AuthStatus.authenticated;

  Future<void> bootstrap() async {
    final refreshToken = await _storage.readRefreshToken();
    lastKnownEmail = await _storage.readLastUserEmail();
    if (refreshToken != null && refreshToken.isNotEmpty) {
      status = AuthStatus.authenticated;
    } else {
      status = AuthStatus.unauthenticated;
    }
    notifyListeners();
  }

  void _handleSessionExpired() {
    status = AuthStatus.unauthenticated;
    notifyListeners();
  }

  Future<bool> login(String email, String password) async {
    busy = true;
    errorMessage = null;
    notifyListeners();
    try {
      final pair = await api.login(email: email, password: password);
      if (pair.mfaRequired && !pair.hasTokens) {
        _pendingEmail = email;
        _pendingPassword = password;
        status = AuthStatus.mfaRequired;
        return false;
      }
      await _persistTokens(pair, email);
      status = AuthStatus.authenticated;
      return true;
    } on ApiException catch (e) {
      errorMessage = e.message;
      status = AuthStatus.unauthenticated;
      return false;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<bool> submitMfaCode(String code) async {
    if (_pendingEmail == null || _pendingPassword == null) {
      errorMessage = 'Session expired, please log in again.';
      status = AuthStatus.unauthenticated;
      notifyListeners();
      return false;
    }
    busy = true;
    errorMessage = null;
    notifyListeners();
    try {
      final pair = await api.login(
        email: _pendingEmail!,
        password: _pendingPassword!,
        mfaCode: code,
      );
      if (!pair.hasTokens) {
        errorMessage = 'Invalid verification code.';
        return false;
      }
      await _persistTokens(pair, _pendingEmail!);
      _pendingEmail = null;
      _pendingPassword = null;
      status = AuthStatus.authenticated;
      return true;
    } on ApiException catch (e) {
      errorMessage = e.message;
      return false;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<User?> register({
    required String email,
    required String password,
    required String displayName,
  }) async {
    busy = true;
    errorMessage = null;
    notifyListeners();
    try {
      final user = await api.register(
        email: email,
        password: password,
        displayName: displayName,
      );
      return user;
    } on ApiException catch (e) {
      errorMessage = e.message;
      return null;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<void> _persistTokens(TokenPair pair, String email) async {
    await _storage.saveTokens(
      accessToken: pair.accessToken!,
      refreshToken: pair.refreshToken!,
    );
    await _storage.saveLastUserEmail(email);
    lastKnownEmail = email;
  }

  Future<void> logout() async {
    busy = true;
    notifyListeners();
    try {
      await api.logout();
    } finally {
      await _storage.clearSession();
      status = AuthStatus.unauthenticated;
      busy = false;
      notifyListeners();
    }
  }

  void cancelMfa() {
    _pendingEmail = null;
    _pendingPassword = null;
    status = AuthStatus.unauthenticated;
    notifyListeners();
  }

  Future<Map<String, String>?> enableMfa() async {
    try {
      return await api.mfaEnable();
    } on ApiException catch (e) {
      errorMessage = e.message;
      return null;
    }
  }

  Future<bool> verifyMfaSetup(String code) async {
    try {
      await api.mfaVerify(code);
      return true;
    } on ApiException catch (e) {
      errorMessage = e.message;
      return false;
    }
  }

  Future<bool> forgotPassword(String email) async {
    try {
      await api.passwordForgot(email);
      return true;
    } on ApiException catch (e) {
      errorMessage = e.message;
      return false;
    }
  }

  Future<bool> resetPassword(String token, String newPassword) async {
    try {
      await api.passwordReset(token: token, newPassword: newPassword);
      return true;
    } on ApiException catch (e) {
      errorMessage = e.message;
      return false;
    }
  }
}
