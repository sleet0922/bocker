import 'package:flutter/material.dart';

import 'login_page.dart';

void main() {
  runApp(const BockerMobileApp());
}

class BockerMobileApp extends StatelessWidget {
  const BockerMobileApp({super.key});

  @override
  Widget build(BuildContext context) {
    const seed = Color(0xff0b57d0);
    return MaterialApp(
      title: 'Bocker Mobile',
      debugShowCheckedModeBanner: false,
      theme: _bockerTheme(
        ColorScheme.fromSeed(
          seedColor: seed,
          dynamicSchemeVariant: DynamicSchemeVariant.tonalSpot,
        ),
      ),
      darkTheme: _bockerTheme(
        ColorScheme.fromSeed(
          seedColor: seed,
          brightness: Brightness.dark,
          dynamicSchemeVariant: DynamicSchemeVariant.tonalSpot,
        ),
      ),
      themeMode: ThemeMode.system,
      home: const LoginPage(),
    );
  }
}

ThemeData _bockerTheme(ColorScheme colors) {
  final light = colors.brightness == Brightness.light;
  return ThemeData(
    colorScheme: colors,
    useMaterial3: true,
    scaffoldBackgroundColor: light ? const Color(0xfff8f9fa) : colors.surface,
    appBarTheme: AppBarTheme(
      centerTitle: false,
      elevation: 0,
      scrolledUnderElevation: 0,
      backgroundColor: light ? const Color(0xffffffff) : colors.surface,
      surfaceTintColor: Colors.transparent,
    ),
    cardTheme: CardThemeData(
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(color: colors.outlineVariant),
      ),
    ),
    dialogTheme: DialogThemeData(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    ),
    inputDecorationTheme: InputDecorationTheme(
      border: const OutlineInputBorder(),
      enabledBorder: OutlineInputBorder(
        borderSide: BorderSide(color: colors.outlineVariant),
      ),
      filled: true,
      fillColor: colors.surfaceContainerLowest,
      isDense: true,
    ),
    dividerTheme: DividerThemeData(color: colors.outlineVariant, space: 1),
    snackBarTheme: const SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
    ),
  );
}
