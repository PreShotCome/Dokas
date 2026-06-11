import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../main.dart' show VS, navRequest;
import '../models/alert.dart';
import '../services/api_service.dart';
import 'drill_detail_screen.dart';

class AlertsScreen extends StatefulWidget {
  const AlertsScreen({super.key});

  @override
  State<AlertsScreen> createState() => _AlertsScreenState();
}

class _AlertsScreenState extends State<AlertsScreen> {
  List<Alert>? _alerts;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final page = await ApiService.instance.getAlerts();
      if (mounted) setState(() {
        _alerts = page.alerts;
        _error = null;
      });
    } on ApiException catch (e) {
      if (!e.unauthorized && mounted) setState(() => _error = e.message);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    }
  }

  void _open(Alert a) {
    if (a.targetKind == 'drill' && a.targetId.isNotEmpty) {
      Navigator.of(context).push(MaterialPageRoute(builder: (_) => DrillDetailScreen(drillId: a.targetId)));
    } else if (a.targetKind == 'heartbeat') {
      navRequest.value = 'heartbeats';
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Alerts')),
      body: RefreshIndicator(
        onRefresh: _load,
        child: _body(),
      ),
    );
  }

  Widget _body() {
    if (_error != null) return _centered(_error!);
    final alerts = _alerts;
    if (alerts == null) return const Center(child: CircularProgressIndicator());
    if (alerts.isEmpty) return _centered('No alerts. Everything is healthy. 🔥');
    return ListView.separated(
      padding: const EdgeInsets.all(16),
      itemCount: alerts.length,
      separatorBuilder: (_, __) => const SizedBox(height: 8),
      itemBuilder: (_, i) => _AlertTile(alerts[i], onTap: () => _open(alerts[i])),
    );
  }

  Widget _centered(String msg) => ListView(children: [
        const SizedBox(height: 160),
        Center(child: Padding(padding: const EdgeInsets.all(24), child: Text(msg, textAlign: TextAlign.center, style: const TextStyle(color: VS.muted)))),
      ]);
}

class _AlertTile extends StatelessWidget {
  final Alert alert;
  final VoidCallback onTap;
  const _AlertTile(this.alert, {required this.onTap});

  @override
  Widget build(BuildContext context) {
    final bad = alert.isBad;
    final color = bad ? VS.red : VS.sage;
    return Card(
      child: ListTile(
        onTap: onTap,
        leading: Icon(bad ? Icons.error : Icons.check_circle, color: color),
        title: Text(alert.title, style: const TextStyle(fontWeight: FontWeight.w600)),
        subtitle: Text(alert.subtitle, style: const TextStyle(color: VS.muted)),
        trailing: Text(DateFormat.MMMd().add_jm().format(alert.at.toLocal()),
            style: const TextStyle(color: VS.muted, fontSize: 12)),
      ),
    );
  }
}
