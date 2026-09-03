import 'package:flutter/material.dart';

import 'screens/home_shell.dart';
import 'services/pocketbase_service.dart';
import 'theme.dart';

void main() {
  runApp(const TradeGuardApp());
}

class TradeGuardApp extends StatefulWidget {
  const TradeGuardApp({super.key});

  @override
  State<TradeGuardApp> createState() => _TradeGuardAppState();
}

class _TradeGuardAppState extends State<TradeGuardApp> {
  late final TradeGuardService service;

  @override
  void initState() {
    super.initState();
    service = TradeGuardService();
  }

  @override
  void dispose() {
    service.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'TradeGuard',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.dark(),
      home: HomeShell(service: service),
    );
  }
}
