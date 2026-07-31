import 'package:shared_preferences/shared_preferences.dart';

/// Small wrapper around [SharedPreferences] for non-sensitive local
/// preferences (as opposed to [SecureStorage], which holds tokens/keys).
class AppSettings {
  AppSettings._();

  static const _kDeviceName = 'nexusvpn.pref.device_name';

  static Future<String> getDeviceName({String fallback = 'My Device'}) async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_kDeviceName) ?? fallback;
  }

  static Future<void> setDeviceName(String name) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_kDeviceName, name);
  }
}
