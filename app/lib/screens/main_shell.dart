import 'package:flutter/material.dart';

import '../main.dart' show navRequest;
import 'alerts_screen.dart';
import 'drills_screen.dart';
import 'heartbeats_screen.dart';
import 'settings_screen.dart';

class MainShell extends StatefulWidget {
  const MainShell({super.key});

  @override
  State<MainShell> createState() => _MainShellState();
}

class _MainShellState extends State<MainShell> {
  int _index = 0;

  // Tab ids align with the deep-link values pushed by PushService.
  static const _tabs = ['alerts', 'drills', 'heartbeats', 'settings'];
  static const _pages = [
    AlertsScreen(),
    DrillsScreen(),
    HeartbeatsScreen(),
    SettingsScreen(),
  ];

  @override
  void initState() {
    super.initState();
    navRequest.addListener(_onNavRequest);
  }

  @override
  void dispose() {
    navRequest.removeListener(_onNavRequest);
    super.dispose();
  }

  void _onNavRequest() {
    final req = navRequest.value;
    if (req == null) return;
    final i = _tabs.indexOf(req);
    if (i >= 0 && mounted) setState(() => _index = i);
    navRequest.value = null;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(index: _index, children: _pages),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (i) => setState(() => _index = i),
        destinations: const [
          NavigationDestination(icon: Icon(Icons.notifications_outlined), selectedIcon: Icon(Icons.notifications), label: 'Alerts'),
          NavigationDestination(icon: Icon(Icons.fact_check_outlined), selectedIcon: Icon(Icons.fact_check), label: 'Drills'),
          NavigationDestination(icon: Icon(Icons.favorite_outline), selectedIcon: Icon(Icons.favorite), label: 'Check-ins'),
          NavigationDestination(icon: Icon(Icons.settings_outlined), selectedIcon: Icon(Icons.settings), label: 'Settings'),
        ],
      ),
    );
  }
}
