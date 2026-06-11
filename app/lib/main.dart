// Entry point + App widget + AuthGate + the VS palette.
//
// Convention (matches plutus-app / tech-support): flat lib/{models,screens,
// services}; no router, no Riverpod/Bloc/Provider. State sharing via top-level
// ValueNotifiers (navRequest). Firebase is used ONLY for push and its init is
// guarded so the app runs without any Firebase config.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:firebase_core/firebase_core.dart';

import 'screens/login_screen.dart';
import 'screens/main_shell.dart';
import 'services/auth_service.dart';
import 'services/push_service.dart';
import 'theme.dart';

// ──────────────────────────────────────────────────────────────────────────
// Color palette — `VS` per PreShotCome convention (PC = plutus, TS = tech-
// support, VS = Vesta). Warm hearth/flame tones: the fire that never goes out.
class VS {
  static const background = Color(0xFF14110F); // warm near-black
  static const surface = Color(0xFF1E1A17); // inputs, tiles
  static const card = Color(0xFF26211D); // cards
  static const flame = Color(0xFFFF6A3D); // primary accent (ember)
  static const ember = Color(0xFFFFA53D); // secondary warm accent
  static const gold = Color(0xFFE8C170); // highlights
  static const sage = Color(0xFF6FBF8E); // healthy / up / passed
  static const red = Color(0xFFE5534B); // down / failed
  static const ink = Color(0xFFF3ECE4); // primary text
  static const muted = Color(0xFF9A8F84); // secondary text
}

// navRequest is the cross-screen tab bus: set its value to a tab id and the
// shell switches tabs (used by push taps to deep-link).
final ValueNotifier<String?> navRequest = ValueNotifier<String?>(null);

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Load config + any stored session first.
  await AuthService.instance.restore();

  // Push is optional. Firebase.initializeApp reads native config
  // (google-services.json / GoogleService-Info.plist); if that isn't present
  // yet, the app still runs — only push is disabled.
  try {
    await Firebase.initializeApp();
    await PushService.instance.init();
    if (AuthService.instance.signedIn.value) {
      await PushService.instance.register();
    }
  } catch (e) {
    debugPrint('Firebase/push not configured — running without push: $e');
  }

  runApp(const VestaApp());
}

class VestaApp extends StatelessWidget {
  const VestaApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Vesta',
      debugShowCheckedModeBanner: false,
      theme: buildVestaTheme(),
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
