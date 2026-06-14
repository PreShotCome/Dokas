# Dokaz — responder app

A native Flutter app (Android + iOS) for incident responders: know the moment a
restore **drill fails** or a backup **check-in goes dark**, from your phone.

It authenticates against Dokaz's **own backend** (`/mobile/login`, not Firebase
Auth) and reads the drill/heartbeat/alert API. Firebase is used **only for push**
and is optional — the app runs without it (push just stays off).

## Layout (matches plutus-app / tech-support conventions)

```
lib/
  main.dart                  # entry, VS palette, AuthGate, navRequest bus
  theme.dart                 # ThemeData
  models/    alert.dart  drill.dart  heartbeat.dart
  services/  app_config.dart   # backend base URL (SharedPreferences)
             auth_service.dart # /mobile/login + /mfa-verify, token in secure storage
             api_service.dart  # Bearer reads: alerts, drills, heartbeats
             push_service.dart # FCM ↔ /mobile/devices, tap → tab deep-link
  screens/   login_screen.dart  main_shell.dart
             alerts_screen.dart drills_screen.dart drill_detail_screen.dart
             heartbeats_screen.dart settings_screen.dart
```

Flat `lib/{models,screens,services}`; no router, no Riverpod/Bloc/Provider —
`setState` + top-level `ValueNotifier`s (`navRequest`, `AuthService.signedIn`).

## First run

This repo holds the Dart source only. Generate the platform scaffolding and
fetch packages on a machine with the Flutter SDK:

```bash
cd app
flutter create .          # generates android/ and ios/ (keeps lib/, pubspec.yaml)
flutter pub get
flutter run
```

The backend URL defaults to `https://app.dokaz.io` and is editable in-app
(Settings). Point it at a local server for development.

## Backend endpoints used

`POST /mobile/login` · `POST /mobile/mfa-verify` · `POST /mobile/logout` ·
`POST /mobile/devices` · `DELETE /mobile/devices/{id}` ·
`GET /mobile/alerts` · `GET /mobile/drills` · `GET /mobile/drills/{id}` ·
`GET /mobile/heartbeats`

## Enabling push (optional, owner step)

1. Create a Firebase project for Dokaz; add an Android app and an iOS app.
2. Drop `android/app/google-services.json` and
   `ios/Runner/GoogleService-Info.plist` in place (git-ignored).
3. iOS: upload an APNs auth key to Firebase (needs a paid Apple Developer
   account).
4. Backend: set `FIREBASE_SERVICE_ACCOUNT` and swap the server's `push.LogSender`
   for the real FCM HTTP v1 sender.

Until then the app builds and runs; device registration and pushes are simply
no-ops.
