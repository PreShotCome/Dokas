// Entry point + App widget + AuthGate + the VS palette.
//
// Convention (matches plutus-app / tech-support): flat lib/{models,screens,
// services}; no router, no Riverpod/Bloc/Provider. State sharing via top-level
// ValueNotifiers (navRequest). Firebase is used ONLY for push and its init is
// guarded so the app runs without any Firebase config.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:firebase_core/firebase_core.dart';

import 'firebase_options.dart';
import 'screens/login_screen.dart';
import 'screens/main_shell.dart';
import 'services/auth_service.dart';
import 'services/push_service.dart';
import 'theme.dart';

// ──────────────────────────────────────────────────────────────────────────
// Color palette — `VS` per PreShotCome convention (PC = plutus, TS = tech-
// support, VS = Dokaz). Steely-gray base with baby-royal-blue primary and
// pink accents; the mark is a turtle (shell = proof your data's protected).
class VS {
  static const background = Color(0xFF1B2027); // steely charcoal
  static const surface = Color(0xFF252C35); // inputs, tiles
  static const card = Color(0xFF2E3640); // cards
  static const blue = Color(0xFF6E9BF0); // primary — baby royal blue
  static const pink = Color(0xFFF48FB1); // accent — pink
  static const steel = Color(0xFF8FA3B8); // steely gray — icons, highlights
  static const up = Color(0xFF56C596); // healthy / passed (cool green)
  static const down = Color(0xFFFF6B81); // down / failed (pink-red)
  static const ink = Color(0xFFEDF1F6); // primary text
  static const muted = Color(0xFF9AA7B6); // secondary text
}

// navRequest is the cross-screen tab bus: set its value to a tab id and the
// shell switches tabs (used by push taps to deep-link).
final ValueNotifier<String?> navRequest = ValueNotifier<String?>(null);

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Load config + any stored session first.
  await AuthService.instance.restore();

  // Push is optional. Firebase is initialized from the generated
  // firebase_options.dart (flutterfire configure; gitignored per-project).
  // Wrapped in try/catch so unsupported platforms (e.g. Windows desktop) or a
  // missing config just disable push — the app still runs.
  try {
    await Firebase.initializeApp(options: DefaultFirebaseOptions.currentPlatform);
    await PushService.instance.init();
    if (AuthService.instance.signedIn.value) {
      await PushService.instance.register();
    }
  } catch (e) {
    debugPrint('Firebase/push not configured — running without push: $e');
  }

  runApp(const DokazApp());
}

class DokazApp extends StatelessWidget {
  const DokazApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Dokaz',
      debugShowCheckedModeBanner: false,
      theme: buildDokazTheme(),
      home: const AuthGate(),
    );
  }
}

// AuthGate swaps between the login screen and the main shell on auth state.
class AuthGate extends StatelessWidget {
  const AuthGate({super.key});

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<bool>(
      valueListenable: AuthService.instance.signedIn,
      builder: (context, signedIn, _) {
        return signedIn ? const MainShell() : const LoginScreen();
      },
    );
  }
}
