/// Heartbeat is a backup check-in monitor, from GET /mobile/heartbeats.
class Heartbeat {
  final String id;
  final String name;
  final String status; // new | up | down | paused
  final bool overdue;
  final int periodSeconds;
  final int graceSeconds;
  final DateTime? lastPingAt;
  final DateTime? expectedBy;

  Heartbeat({
    required this.id,
    required this.name,
    required this.status,
    required this.overdue,
    required this.periodSeconds,
    required this.graceSeconds,
    required this.lastPingAt,
    required this.expectedBy,
  });

  factory Heartbeat.fromJson(Map<String, dynamic> j) => Heartbeat(
        id: j['id'] as String,
        name: (j['name'] ?? '') as String,
        status: j['status'] as String,
        overdue: (j['overdue'] ?? false) as bool,
        periodSeconds: (j['period_seconds'] ?? 0) as int,
        graceSeconds: (j['grace_seconds'] ?? 0) as int,
        lastPingAt: _dt(j['last_ping_at']),
        expectedBy: _dt(j['expected_by']),
      );

  bool get isDown => status == 'down' || overdue;
  bool get isUp => status == 'up' && !overdue;
}

DateTime? _dt(dynamic v) => (v is String && v.isNotEmpty) ? DateTime.tryParse(v) : null;
