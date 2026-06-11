import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../main.dart' show VS;
import '../models/heartbeat.dart';
import '../services/api_service.dart';

class HeartbeatsScreen extends StatefulWidget {
  const HeartbeatsScreen({super.key});

  @override
  State<HeartbeatsScreen> createState() => _HeartbeatsScreenState();
}

class _HeartbeatsScreenState extends State<HeartbeatsScreen> {
  List<Heartbeat>? _items;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final items = await ApiService.instance.getHeartbeats();
      if (mounted) setState(() {
        _items = items;
        _error = null;
      });
    } on ApiException catch (e) {
      if (!e.unauthorized && mounted) setState(() => _error = e.message);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Check-ins')),
      body: RefreshIndicator(onRefresh: _load, child: _body()),
    );
  }

  Widget _body() {
    if (_error != null) return _centered(_error!);
    final items = _items;
    if (items == null) return const Center(child: CircularProgressIndicator());
    if (items.isEmpty) return _centered('No backup check-ins configured.');
    // Down monitors float to the top.
    final sorted = [...items]..sort((a, b) => (b.isDown ? 1 : 0).compareTo(a.isDown ? 1 : 0));
    return ListView.separated(
      padding: const EdgeInsets.all(16),
      itemCount: sorted.length,
      separatorBuilder: (_, __) => const SizedBox(height: 8),
      itemBuilder: (_, i) => _HeartbeatTile(sorted[i]),
    );
  }

  Widget _centered(String m) => ListView(children: [
        const SizedBox(height: 160),
        Center(child: Text(m, style: const TextStyle(color: VS.muted))),
      ]);
}

class _HeartbeatTile extends StatelessWidget {
  final Heartbeat hb;
  const _HeartbeatTile(this.hb);

  @override
  Widget build(BuildContext context) {
    final (label, color) = hb.isDown
        ? ('DOWN', VS.red)
        : hb.status == 'paused'
            ? ('PAUSED', VS.muted)
            : hb.status == 'new'
                ? ('NEW', VS.ember)
                : ('UP', VS.sage);
    final last = hb.lastPingAt == null
        ? 'no check-in yet'
        : 'last ${DateFormat.MMMd().add_jm().format(hb.lastPingAt!.toLocal())}';
    return Card(
      child: ListTile(
        leading: Icon(hb.isDown ? Icons.heart_broken : Icons.favorite, color: color),
        title: Text(hb.name, style: const TextStyle(fontWeight: FontWeight.w600)),
        subtitle: Text(last, style: const TextStyle(color: VS.muted)),
        trailing: Text(label, style: TextStyle(color: color, fontWeight: FontWeight.w700)),
      ),
    );
  }
}
