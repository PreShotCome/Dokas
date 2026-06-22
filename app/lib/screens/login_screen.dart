import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

import '../main.dart' show VS;
import '../services/app_config.dart';
import '../services/auth_service.dart';

class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _email = TextEditingController();
  final _password = TextEditingController();
  final _code = TextEditingController();

  bool _busy = false;
  String? _error;
  String? _challengeId; // non-null once the password step asks for MFA

  Future<void> _submit() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    final auth = AuthService.instance;
    final result = _challengeId == null
        ? await auth.login(_email.text.trim(), _password.text)
        : await auth.verifyMfa(_challengeId!, _code.text.trim());

    if (!mounted) return;
    setState(() => _busy = false);
    if (result.mfaRequired) {
      setState(() => _challengeId = result.challengeId);
      return;
    }
    if (!result.ok) {
      setState(() => _error = result.error ?? 'Sign-in failed');
    }
    // On success, AuthGate swaps to the shell automatically.
  }

  // Lets the responder point the app at their Dokaz backend (e.g. a local dev
  // server) without rebuilding. Persisted in SharedPreferences via AppConfig.
  Future<void> _editServer() async {
    final ctrl = TextEditingController(text: AppConfig.instance.baseUrl);
    final saved = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Server URL'),
        content: TextField(
          controller: ctrl,
          autocorrect: false,
          keyboardType: TextInputType.url,
          decoration: const InputDecoration(hintText: 'https://app.dokaz.net'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Save')),
        ],
      ),
    );
    if (saved == true) {
      await AppConfig.instance.setBaseUrl(ctrl.text);
      if (mounted) setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final mfa = _challengeId != null;
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 380),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  SvgPicture.asset('assets/turtle.svg', height: 72),
                  const SizedBox(height: 12),
                  const Text('Dokaz',
                      textAlign: TextAlign.center,
                      style: TextStyle(fontSize: 30, fontWeight: FontWeight.w700, color: VS.ink)),
                  const SizedBox(height: 4),
                  const Text('Know the moment a backup fails.',
                      textAlign: TextAlign.center, style: TextStyle(color: VS.muted)),
                  const SizedBox(height: 32),
                  if (!mfa) ...[
                    TextField(
                      controller: _email,
                      keyboardType: TextInputType.emailAddress,
                      autocorrect: false,
                      decoration: const InputDecoration(hintText: 'Email'),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _password,
                      obscureText: true,
                      onSubmitted: (_) => _submit(),
                      decoration: const InputDecoration(hintText: 'Password'),
                    ),
                  ] else ...[
                    const Text('Enter the 6-digit code from your authenticator app.',
                        style: TextStyle(color: VS.muted)),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _code,
                      keyboardType: TextInputType.number,
                      autofocus: true,
                      onSubmitted: (_) => _submit(),
                      decoration: const InputDecoration(hintText: '123456'),
                    ),
                  ],
                  if (_error != null) ...[
                    const SizedBox(height: 12),
                    Text(_error!, style: const TextStyle(color: VS.down)),
                  ],
                  const SizedBox(height: 20),
                  ElevatedButton(
                    onPressed: _busy ? null : _submit,
                    child: _busy
                        ? const SizedBox(
                            height: 20, width: 20, child: CircularProgressIndicator(strokeWidth: 2))
                        : Text(mfa ? 'Verify' : 'Sign in'),
                  ),
                  const SizedBox(height: 8),
                  TextButton(
                    onPressed: _editServer,
                    child: Text('Server: ${AppConfig.instance.baseUrl}',
                        style: const TextStyle(color: VS.muted, fontSize: 12)),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
