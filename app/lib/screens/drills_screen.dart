import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../main.dart' show VS;
import '../models/drill.dart';
import '../services/api_service.dart';
import 'drill_detail_screen.dart';

class DrillsScreen extends StatefulWidget {
  const DrillsScreen({super.key});

  @override
  State<DrillsScreen> createState() => _DrillsScreenState();
}

class _DrillsScreenState extends State<DrillsScreen> {
  List<Drill>? _drills;
  String? _error;
  String _filter = 'all'; // all | failed | succeeded
  bool _newestFirst = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final drills = await ApiService.instance.getDrills();
      if (mounted) setState(() {
        _drills = drills;
        _error = null;
      });
    } on ApiException catch (e) {
      if (!e.unauthorized && mounted) setState(() => _error = e.message);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    }
  }

  List<Drill> get _visible {
    final list = [...?_drills];
    final filtered = _filter == 'all' ? list : list.where((d) => d.status == _filter).toList();
    filtered.sort((a, b) => _newestFirst
        ? b.createdAt.compareTo(a.createdAt)
        : a.createdAt.compareTo(b.createdAt));
    return filtered;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Drills'),
        actions: [
          IconButton(
            tooltip: _newestFirst ? 'Newest first' : 'Oldest first',
            icon: Icon(_newestFirst ? Icons.arrow_downward : Icons.arrow_upward),
            onPressed: () => setState(() => _newestFirst = !_newestFirst),
          ),
        ],
      ),
      body: Column(
        children: [
          _filterBar(),
          Expanded(child: RefreshIndicator(onRefresh: _load, child: _body())),
        ],
      ),
    );
  }

  Widget _filterBar() => Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12),
        child: Row(
          children: [
            for (final f in const ['all', 'failed', 'succeeded'])
              Padding(
                padding: const EdgeInsets.only(right: 8),
                child: ChoiceChip(
                  label: Text(f == 'succeeded' ? 'passed' : f),
                  selected: _filter == f,
                  onSelected: (_) => setState(() => _filter = f),
                ),
              ),
          ],
        ),
      );

  Widget _body() {
    if (_error != null) return _centered(_error!);
    if (_drills == null) return const Center(child: CircularProgressIndicator());
    final visible = _visible;
    if (visible.isEmpty) return _centered('No drills yet.');
    return ListView.separated(
      padding: const EdgeInsets.all(16),
      itemCount: visible.length,
      separatorBuilder: (_, __) => const SizedBox(height: 8),
      itemBuilder: (_, i) => _DrillTile(visible[i]),
    );
  }

  Widget _centered(String m) => ListView(children: [
        const SizedBox(height: 160),
        Center(child: Text(m, style: const TextStyle(color: VS.muted))),
      ]);
}

class _DrillTile extends StatelessWidget {
  final Drill drill;
  const _DrillTile(this.drill);

  @override
  Widget build(BuildContext context) {
    final (icon, color) = switch (drill.status) {
      'failed' => (Icons.error, VS.red),
      'succeeded' => (Icons.check_circle, VS.sage),
      'running' => (Icons.sync, VS.ember),
      _ => (Icons.schedule, VS.muted),
    };
    return Card(
      child: ListTile(
        leading: Icon(icon, color: color),
        title: Text('Drill ${drill.id.substring(0, 8)}', style: const TextStyle(fontWeight: FontWeight.w600)),
        subtitle: Text(
          drill.error ?? DateFormat.MMMd().add_jm().format(drill.createdAt.toLocal()),
          style: const TextStyle(color: VS.muted),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        trailing: Text(drill.status, style: TextStyle(color: color, fontWeight: FontWeight.w600)),
        onTap: () => Navigator.of(context)
            .push(MaterialPageRoute(builder: (_) => DrillDetailScreen(drillId: drill.id))),
      ),
    );
  }
}
