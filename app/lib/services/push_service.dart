import 'dart:convert';
import 'dart:io' show Platform;

import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import '../main.dart' show navRequest;
import 'app_config.dart';
import 'auth_service.dart';

/// PushService wires Firebase Cloud Messaging to the backend device registry
/// and routes a tapped notification to the right tab. Entirely optional: if
/// Firebase isn't configured, init() throws and main() swallows it.
class PushService {
  PushService._();
  static final PushService instance = PushService._();

  final _storage = const FlutterSecureStorage();
  static const _deviceIdKey = 'vesta_device_id';

  bool _ready = false;
  String? _fcmToken;

  Future<void> init() async {
    final messaging = FirebaseMessaging.instance;
    await messaging.requestPermission();
    _fcmToken = await messaging.getToken();

    // A refreshed token must be re-registered while signed in.
    messaging.onTokenRefresh.listen((t) {
      _fcmToken = t;
      if (AuthService.instance.token != null) register();
    });

    FirebaseMessaging.onMessageOpenedApp.listen(_handleTap);
    final initial = await messaging.getInitialMessage();
    if (initial != null) _handleTap(initial);

    _ready = true;
  }

  /// register uploads the current FCM token to /mobile/devices. Safe to call on
  /// every login/launch — the backend upserts.
  Future<void> register() async {
    if (!_ready || _fcmToken == null) return;
    final auth = AuthService.instance.token;
    if (auth == null) return;
    try {
      final resp = await http.post(
        Uri.parse('${AppConfig.instance.baseUrl}/mobile/devices'),
        headers: {'Authorization': 'Bearer $auth', 'Content-Type': 'application/json'},
        body: jsonEncode({'token': _fcmToken, 'platform': _platform()}),
      );
      if (resp.statusCode == 200) {
        final id = (jsonDecode(resp.body)['data']?['id']) as String?;
        if (id != null) await _storage.write(key: _deviceIdKey, value: id);
      }
    } catch (e) {
      debugPrint('push register failed: $e');
    }
  }

  /// unregister removes this device from the backend (on logout).
  Future<void> unregister() async {
    final id = await _storage.read(key: _deviceIdKey);
    final auth = AuthService.instance.token;
    if (id == null || auth == null) return;
    try {
      await http.delete(
        Uri.parse('${AppConfig.instance.baseUrl}/mobile/devices/$id'),
        headers: {'Authorization': 'Bearer $auth'},
      );
    } catch (_) {}
    await _storage.delete(key: _deviceIdKey);
  }

  void _handleTap(RemoteMessage m) {
    switch (m.data['type']) {
      case 'heartbeat':
        navRequest.value = 'heartbeats';
        break;
      case 'drill':
        navRequest.value = 'drills';
        break;
      default:
        navRequest.value = 'alerts';
    }
  }

  String _platform() {
    if (kIsWeb) return 'web';
    if (Platform.isIOS) return 'ios';
    if (Platform.isAndroid) return 'android';
    return 'other';
  }
}
