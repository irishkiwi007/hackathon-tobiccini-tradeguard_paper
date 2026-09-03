import 'package:flutter/material.dart';

class AppTheme {
  static const Color buy = Color(0xFF16A34A);
  static const Color sell = Color(0xFFDC2626);
  static const Color warn = Color(0xFFF59E0B);

  static ThemeData dark() {
    final base = ThemeData(
      brightness: Brightness.dark,
      useMaterial3: true,
      colorSchemeSeed: const Color(0xFF2563EB),
      scaffoldBackgroundColor: const Color(0xFF0B0F14),
    );

    return base.copyWith(
      cardTheme: base.cardTheme.copyWith(
        color: const Color(0xFF141A21),
        elevation: 0,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
      navigationBarTheme: base.navigationBarTheme.copyWith(
        backgroundColor: const Color(0xFF141A21),
        indicatorColor: const Color(0xFF2563EB).withOpacity(0.25),
      ),
      appBarTheme: base.appBarTheme.copyWith(
        backgroundColor: const Color(0xFF0B0F14),
        elevation: 0,
      ),
    );
  }
}
