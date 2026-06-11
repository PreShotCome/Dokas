/// Alert is one entry of the responder feed (a drill outcome or a heartbeat
/// liveness change), mapped from GET /mobile/alerts.
class Alert {
  final String id;
  final DateTime at;
  final String action; // drill.failed | drill.completed | heartbeat.down | heartbeat.up
  final String targetKind; // "drill" | "heartbeat"
  final String targetId;
  final Map<String, dynamic> metadata;

  Alert({
    required this.id,
    required this.at,
    required this.action,
    required this.targetKind,
    required this.targetId,
    required this.metadata,
  });

  factory Alert.fromJson(Map<String, dynamic> j) => Alert(
        id: j['id'] as String,
        at: DateTime.parse(j['at'] as String),
        action: j['action'] as String,
        targetKind: (j['target_kind'] ?? '') as String,
        targetId: (j['target_id'] ?? '') as String,
        metadata: (j['metadata'] as Map<String, dynamic>?) ?? const {},
      );

  bool get isBad => action == 'drill.failed' || action == 'heartbeat.down';

  String get title {
    switch (action) {
      case 'drill.failed':
        return 'Drill failed';
      case 'drill.completed':
        return 'Drill passed';
      case 'heartbeat.down':
        return 'Backup check-in down';
      case 'heartbeat.up':
        return 'Backup check-in recovered';
      default:
        return action;
    }
  }

  String get subtitle {
    final reason = metadata['reason'];
    if (reason is String && reason.isNotEmpty) return reason.replaceAll('_', ' ');
    return targetKind.isEmpty ? '' : '$targetKind ${_short(targetId)}';
  }

  static String _short(String id) => id.length > 8 ? id.substring(0, 8) : id;
}
