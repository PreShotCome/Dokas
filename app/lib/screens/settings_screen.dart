import 'package:flutter/material.dart';

import '../main.dart' show VS;
import '../services/app_config.dart';
import '../services/auth_service.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final TextEditingController _url =
      TextEditingController(text: AppConfig.instance.baseUrl);

  Future<void> _saveUrl() async {
    await AppConfig.instance.setBaseUrl(_url.text);
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Backend URL saved')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final email = AuthService.instance.email ?? '';
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          const Text('Signed in as', style: TextStyle(color: VS.muted)),
          const SizedBox(height: 4),
          Text(email, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
          const SizedBox(height: 28),
          const Text('Backend URL', style: TextStyle(color: VS.muted)),
          const SizedBox(height: 6),
          TextField(
            controller: _url,
            keyboardType: TextInputType.url,
            autocorrect: false,
            decoration: InputDecoration(
              suffixIcon: IconButton(icon: const Icon(Icons.save, color: VS.blue), onPressed: _saveUrl),
            ),
          ),
          const SizedBox(height: 36),
          OutlinedButton.icon(
            style: OutlinedButton.styleFrom(
              foregroundColor: VS.down,
              side: const BorderSide(color: VS.down),
              padding: const EdgeInsets.symmetric(vertical: 14),
            ),
            icon: const Icon(Icons.logout),
            label: const Text('Sign out'),
            onPressed: () => AuthService.instance.logout(),
          ),
        ],
      ),
    );
  }
}
