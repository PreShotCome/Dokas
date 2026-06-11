import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../main.dart' show VS;
import '../models/drill.dart';
import '../services/api_service.dart';

class DrillDetailScreen extends StatefulWidget {
  final String drillId;
  const DrillDetailScreen({super.key, required this.drillId});

  @override
  State<DrillDetailScreen> createState() => _DrillDetailScreenState();
}

class _DrillDetailScreenState extends State<DrillDetailScreen> {
  DrillDetail? _drill;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final d = await ApiService.instance.getDrill(widget.drillId);
      if (mounted) setState(() => _drill = d);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Drill')),
      body: _error != null
          ? Center(child: Text(_error!, style: const TextStyle(color: VS.red)))
          : _drill == null
              ? const Center(child: CircularProgressIndicator())
              : _detail(_drill!),
    );
  }

  Widget _detail(DrillDetail d) {
    final (label, color) = d.failed
        ? ('FAILED', VS.red)
        : d.passed
            ? ('PASSED', VS.sage)
            : (d.status.toUpperCase(), VS.muted);
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Row(children: [
          Icon(d.failed ? Icons.error : Icons.check_circle, color: color, size: 28),
          const SizedBox(width: 10),
          Text(label, style: TextStyle(color: color, fontWeight: FontWeight.w700, fontSize: 20)),
        ]),
        const SizedBox(height: 4),
        Text('Drill ${d.id.substring(0, 8)} · ${DateFormat.yMMMd().add_jm().format(d.createdAt.toLocal())}',
            style: const TextStyle(color: VS.muted)),
        if (d.error != null) ...[
          const SizedBox(height: 12),
          Card(color: VS.red.withOpacity(0.12), child: Padding(
            padding: const EdgeInsets.all(12),
            child: Text(d.error!, style: const TextStyle(color: VS.red)),
          )),
        ],
        const SizedBox(height: 20),
        _section('Steps'),
        ...d.steps.map(_stepRow),
        const SizedBox(height: 20),
        _section('Assertions'),
        if (d.assertions.isEmpty)
          const Padding(padding: EdgeInsets.symmetric(vertical: 8), child: Text('No assertions recorded.', style: TextStyle(color: VS.muted)))
        else
          ...d.assertions.map(_assertionRow),
        const SizedBox(height: 20),
        if (d.hasEvidence)
          Row(children: const [
            Icon(Icons.verified, color: VS.gold, size: 18),
            SizedBox(width: 8),
            Expanded(child: Text('Signed Proof-of-Recovery evidence available.', style: TextStyle(color: VS.gold))),
          ]),
      ],
    );
  }

  Widget _section(String t) => Padding(
        padding: const EdgeInsets.only(bottom: 8),
        child: Text(t, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
      );

  Widget _stepRow(DrillStep s) {
    final (icon, color) = switch (s.status) {
      'succeeded' => (Icons.check, VS.sage),
      'failed' => (Icons.close, VS.red),
      'skipped' => (Icons.remove, VS.muted),
      'running' => (Icons.sync, VS.ember),
      _ => (Icons.schedule, VS.muted),
    };
    return ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      leading: Icon(icon, color: color, size: 20),
      title: Text(s.name),
      subtitle: s.error != null ? Text(s.error!, style: const TextStyle(color: VS.red)) : null,
    );
  }

  Widget _assertionRow(DrillAssertion a) => ListTile(
        dense: true,
        contentPadding: EdgeInsets.zero,
        leading: Icon(a.passed ? Icons.check : Icons.close, color: a.passed ? VS.sage : VS.red, size: 20),
        title: Text(a.kind),
        subtitle: Text('expected ${a.expected}  ·  actual ${a.actual}', style: const TextStyle(color: VS.muted)),
      );
}
