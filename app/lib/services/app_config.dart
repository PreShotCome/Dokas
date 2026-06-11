import 'package:shared_preferences/shared_preferences.dart';

/// AppConfig holds the backend base URL, persisted in SharedPreferences so a
/// user (or QA) can point the app at staging/local without a rebuild.
class AppConfig {
  AppConfig._();
  static final AppConfig instance = AppConfig._();

  static const _key = 'backend_base_url';
  static const _default = 'https://app.vesta.io';

  String _baseUrl = _default;
  String get baseUrl => _baseUrl;

  Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    _baseUrl = prefs.getString(_key) ?? _default;
  }

  Future<void> setBaseUrl(String url) async {
    _baseUrl = url.trim().replaceAll(RegExp(r'/+$'), '');
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, _baseUrl);
  }
}
