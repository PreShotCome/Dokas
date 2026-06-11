import 'dart:convert';

import 'package:http/http.dart' as http;

import '../models/alert.dart';
import '../models/drill.dart';
import '../models/heartbeat.dart';
import 'app_config.dart';
import 'auth_service.dart';

class ApiException implements Exception {
  final String message;
  final bool unauthorized;
  ApiException(this.message, {this.unauthorized = false});
  @override
  String toString() => message;
}

/// A page of alerts plus the keyset cursor for the next, older page.
class AlertsPage {
  final List<Alert> alerts;
  final String? nextCursor;
  AlertsPage(this.alerts, this.nextCursor);
}

/// ApiService calls the token-authenticated /mobile read API.
class ApiService {
  ApiService._();
  static final ApiService instance = ApiService._();

  Map<String, String> get _headers {
    final t = AuthService.instance.token;
    return {'Authorization': 'Bearer $t'};
  }

  Uri _u(String path, [Map<String, String>? query]) {
    final base = Uri.parse('${AppConfig.instance.baseUrl}$path');
    return (query == null || query.isEmpty) ? base : base.replace(queryParameters: query);
  }

  Future<Map<String, dynamic>> _get(String path, [Map<String, String>? query]) async {
    final resp = await http.get(_u(path, query), headers: _headers);
    if (resp.statusCode == 401) {
      // Token expired/revoked — drop to the login screen.
      await AuthService.instance.logout();
      throw ApiException('Session expired — please sign in again.', unauthorized: true);
    }
    if (resp.statusCode != 200) {
      throw ApiException('Request failed (${resp.statusCode}).');
    }
    return jsonDecode(resp.body) as Map<String, dynamic>;
  }

  Future<AlertsPage> getAlerts({String? cursor}) async {
    final body = await _get('/mobile/alerts', {if (cursor != null) 'cursor': cursor});
    final data = (body['data'] as List).map((e) => Alert.fromJson(e as Map<String, dynamic>)).toList();
    final next = (body['meta'] as Map<String, dynamic>?)?['next_cursor'] as String?;
    return AlertsPage(data, next);
  }

  Future<List<Drill>> getDrills() async {
    final body = await _get('/mobile/drills');
    return (body['data'] as List).map((e) => Drill.fromJson(e as Map<String, dynamic>)).toList();
  }

  Future<DrillDetail> getDrill(String id) async {
    final body = await _get('/mobile/drills/$id');
    return DrillDetail.fromJson(body['data'] as Map<String, dynamic>);
  }

  Future<List<Heartbeat>> getHeartbeats() async {
    final body = await _get('/mobile/heartbeats');
    return (body['data'] as List).map((e) => Heartbeat.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// The full URL of a drill's signed evidence PDF (opened in a browser/viewer).
  String evidenceUrl(String drillId) => '${AppConfig.instance.baseUrl}/mobile/drills/$drillId/evidence';
}
