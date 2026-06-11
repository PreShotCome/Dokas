import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import 'app_config.dart';
import 'push_service.dart';

/// Outcome of a login attempt.
class LoginResult {
  final bool ok;
  final bool mfaRequired;
  final String? challengeId;
  final String? error;
  const LoginResult._({this.ok = false, this.mfaRequired = false, this.challengeId, this.error});

  factory LoginResult.success() => const LoginResult._(ok: true);
  factory LoginResult.mfa(String challengeId) =>
      LoginResult._(mfaRequired: true, challengeId: challengeId);
  factory LoginResult.failure(String error) => LoginResult._(error: error);
}

/// AuthService authenticates against Vesta's own backend (/mobile/login),
/// stores the bearer token in secure storage, and exposes signed-in state.
class AuthService {
  AuthService._();
  static final AuthService instance = AuthService._();

  final _storage = const FlutterSecureStorage();
  static const _tokenKey = 'vesta_token';
  static const _emailKey = 'vesta_email';
  static const _accountKey = 'vesta_account';

  final ValueNotifier<bool> signedIn = ValueNotifier<bool>(false);

  String? _token;
  String? email;
  String? accountId;

  String? get token => _token;

  /// restore loads config + any stored token at startup.
  Future<void> restore() async {
    await AppConfig.instance.load();
    _token = await _storage.read(key: _tokenKey);
    email = await _storage.read(key: _emailKey);
    accountId = await _storage.read(key: _accountKey);
    signedIn.value = _token != null;
  }

  Uri _u(String path) => Uri.parse('${AppConfig.instance.baseUrl}$path');

  Future<LoginResult> login(String email, String password, {String deviceName = 'Vesta app'}) async {
    try {
      final resp = await http.post(
        _u('/mobile/login'),
        headers: const {'Content-Type': 'application/json'},
        body: jsonEncode({'email': email, 'password': password, 'device_name': deviceName}),
      );
      final body = _decode(resp.body);
      if (resp.statusCode == 202 && body['data']?['mfa_required'] == true) {
        return LoginResult.mfa(body['data']['challenge_id'] as String);
      }
      if (resp.statusCode == 200) {
        await _persist(body['data']);
        return LoginResult.success();
      }
      return LoginResult.failure(_errorMsg(body, 'Invalid email or password'));
    } catch (e) {
      return LoginResult.failure('Network error: $e');
    }
  }

  Future<LoginResult> verifyMfa(String challengeId, String code) async {
    try {
      final resp = await http.post(
        _u('/mobile/mfa-verify'),
        headers: const {'Content-Type': 'application/json'},
        body: jsonEncode({'challenge_id': challengeId, 'code': code}),
      );
      final body = _decode(resp.body);
      if (resp.statusCode == 200) {
        await _persist(body['data']);
        return LoginResult.success();
      }
      return LoginResult.failure(_errorMsg(body, 'Invalid code'));
    } catch (e) {
      return LoginResult.failure('Network error: $e');
    }
  }

  Future<void> logout() async {
    final t = _token;
    if (t != null) {
      try {
        await http.post(_u('/mobile/logout'), headers: {'Authorization': 'Bearer $t'});
      } catch (_) {}
    }
    await PushService.instance.unregister();
    await _storage.deleteAll();
    _token = null;
    email = null;
    accountId = null;
    signedIn.value = false;
  }

  Future<void> _persist(Map<String, dynamic> data) async {
    _token = data['token'] as String;
    email = (data['user']?['email'] ?? '') as String;
    accountId = data['account_id'] as String?;
    await _storage.write(key: _tokenKey, value: _token);
    await _storage.write(key: _emailKey, value: email);
    if (accountId != null) await _storage.write(key: _accountKey, value: accountId);
    signedIn.value = true;
    // Register this device for push now that we have a token.
    await PushService.instance.register();
  }

  Map<String, dynamic> _decode(String body) {
    if (body.isEmpty) return {};
    return jsonDecode(body) as Map<String, dynamic>;
  }

  String _errorMsg(Map<String, dynamic> body, String fallback) {
    final errors = body['errors'];
    if (errors is List && errors.isNotEmpty) {
      return (errors.first['message'] ?? fallback) as String;
    }
    return fallback;
  }
}
