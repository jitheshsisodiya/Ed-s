import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Small wrapper around [SharedPreferences] for non-sensitive local
/// preferences (as opposed to [SecureStorage], which holds tokens/keys).
class AppSettings {
  AppSettings._();

  static const _kDeviceName = 'nexusvpn.pref.device_name';
  static const _kAdvanced = 'nexusvpn.pref.advanced';
  static const _kThemeMode = 'nexusvpn.pref.theme_mode';
  static const _kLastNetwork = 'nexusvpn.pref.last_network';

  static Future<String> getDeviceName({String fallback = 'My Device'}) async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_kDeviceName) ?? fallback;
  }

  static Future<void> setDeviceName(String name) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_kDeviceName, name);
  }
}

/// Presentation preferences the whole app reads: whether to show the
/// networking detail most people never need, which theme to paint, and the
/// network the big connect control should target.
///
/// These live here rather than in [AppSettings] because the UI has to rebuild
/// when they change; losing one costs a user nothing, so they stay in plain
/// shared preferences.
class Preferences extends ChangeNotifier {
  Preferences({SharedPreferences? prefs}) : _prefs = prefs;

  SharedPreferences? _prefs;

  bool _advanced = false;
  ThemeMode _themeMode = ThemeMode.system;
  String? _lastNetworkId;

  /// Whether to reveal addresses, routes and connection internals.
  bool get advanced => _advanced;

  ThemeMode get themeMode => _themeMode;

  /// The network this device last connected to, so the home screen always
  /// has something for its one control to act on.
  String? get lastNetworkId => _lastNetworkId;

  Future<void> load() async {
    final prefs = _prefs ??= await SharedPreferences.getInstance();
    _advanced = prefs.getBool(AppSettings._kAdvanced) ?? false;
    _themeMode = _parseThemeMode(prefs.getString(AppSettings._kThemeMode));
    _lastNetworkId = prefs.getString(AppSettings._kLastNetwork);
    notifyListeners();
  }

  Future<void> setAdvanced(bool value) async {
    if (_advanced == value) return;
    _advanced = value;
    notifyListeners();
    final prefs = _prefs ??= await SharedPreferences.getInstance();
    await prefs.setBool(AppSettings._kAdvanced, value);
  }

  Future<void> setThemeMode(ThemeMode mode) async {
    if (_themeMode == mode) return;
    _themeMode = mode;
    notifyListeners();
    final prefs = _prefs ??= await SharedPreferences.getInstance();
    await prefs.setString(AppSettings._kThemeMode, mode.name);
  }

  Future<void> setLastNetworkId(String id) async {
    if (_lastNetworkId == id) return;
    _lastNetworkId = id;
    notifyListeners();
    final prefs = _prefs ??= await SharedPreferences.getInstance();
    await prefs.setString(AppSettings._kLastNetwork, id);
  }

  static ThemeMode _parseThemeMode(String? name) {
    return switch (name) {
      'light' => ThemeMode.light,
      'dark' => ThemeMode.dark,
      _ => ThemeMode.system,
    };
  }
}
