/// Drill is the list view of a restore drill, from GET /mobile/drills.
class Drill {
  final String id;
  final String databaseId;
  final String status; // pending | running | succeeded | failed | skipped
  final DateTime? startedAt;
  final DateTime? completedAt;
  final String? error;
  final bool hasEvidence;
  final DateTime createdAt;

  Drill({
    required this.id,
    required this.databaseId,
    required this.status,
    required this.startedAt,
    required this.completedAt,
    required this.error,
    required this.hasEvidence,
    required this.createdAt,
  });

  factory Drill.fromJson(Map<String, dynamic> j) => Drill(
        id: j['id'] as String,
        databaseId: (j['database_id'] ?? '') as String,
        status: j['status'] as String,
        startedAt: _dt(j['started_at']),
        completedAt: _dt(j['completed_at']),
        error: j['error'] as String?,
        hasEvidence: (j['has_evidence'] ?? false) as bool,
        createdAt: _dt(j['created_at']) ?? DateTime.now(),
      );

  bool get failed => status == 'failed';
  bool get passed => status == 'succeeded';
}

/// One step of a drill's pipeline.
class DrillStep {
  final String name;
  final String status;
  final String? error;
  DrillStep({required this.name, required this.status, this.error});

  factory DrillStep.fromJson(Map<String, dynamic> j) => DrillStep(
        name: j['name'] as String,
        status: j['status'] as String,
        error: j['error'] as String?,
      );
}

/// One assertion result of a drill.
class DrillAssertion {
  final String kind;
  final bool passed;
  final dynamic expected;
  final dynamic actual;
  DrillAssertion({required this.kind, required this.passed, this.expected, this.actual});

  factory DrillAssertion.fromJson(Map<String, dynamic> j) => DrillAssertion(
        kind: (j['kind'] ?? '') as String,
        passed: (j['passed'] ?? false) as bool,
        expected: j['expected'],
        actual: j['actual'],
      );
}

/// DrillDetail adds steps + assertions, from GET /mobile/drills/{id}.
class DrillDetail extends Drill {
  final List<DrillStep> steps;
  final List<DrillAssertion> assertions;

  DrillDetail({
    required super.id,
    required super.databaseId,
    required super.status,
    required super.startedAt,
    required super.completedAt,
    required super.error,
    required super.hasEvidence,
    required super.createdAt,
    required this.steps,
    required this.assertions,
  });

  factory DrillDetail.fromJson(Map<String, dynamic> j) {
    final base = Drill.fromJson(j);
    return DrillDetail(
      id: base.id,
      databaseId: base.databaseId,
      status: base.status,
      startedAt: base.startedAt,
      completedAt: base.completedAt,
      error: base.error,
      hasEvidence: base.hasEvidence,
      createdAt: base.createdAt,
      steps: ((j['steps'] as List?) ?? [])
          .map((s) => DrillStep.fromJson(s as Map<String, dynamic>))
          .toList(),
      assertions: ((j['assertions'] as List?) ?? [])
          .map((a) => DrillAssertion.fromJson(a as Map<String, dynamic>))
          .toList(),
    );
  }
}

DateTime? _dt(dynamic v) => (v is String && v.isNotEmpty) ? DateTime.tryParse(v) : null;
