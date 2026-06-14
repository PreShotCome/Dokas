import 'package:flutter/material.dart';

import 'main.dart' show VS;

/// Conservative ThemeData — avoids API that varies across Flutter versions so
/// it builds cleanly on any SDK >= 3.3.
ThemeData buildDokazTheme() {
  final base = ThemeData.dark(useMaterial3: true);
  return base.copyWith(
    scaffoldBackgroundColor: VS.background,
    colorScheme: base.colorScheme.copyWith(
      primary: VS.flame,
      secondary: VS.ember,
      surface: VS.surface,
      error: VS.red,
    ),
    appBarTheme: const AppBarTheme(
      backgroundColor: VS.background,
      foregroundColor: VS.ink,
      elevation: 0,
      centerTitle: false,
    ),
    cardColor: VS.card,
    textTheme: base.textTheme.apply(bodyColor: VS.ink, displayColor: VS.ink),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: VS.surface,
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: BorderSide.none,
      ),
      hintStyle: const TextStyle(color: VS.muted),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        backgroundColor: VS.flame,
        foregroundColor: Colors.black,
        padding: const EdgeInsets.symmetric(vertical: 16),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        textStyle: const TextStyle(fontWeight: FontWeight.w600, fontSize: 16),
      ),
    ),
  );
}
