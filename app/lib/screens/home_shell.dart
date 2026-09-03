import 'package:flutter/material.dart';

import '../services/pocketbase_service.dart';
import 'approval_queue_screen.dart';
import 'audit_trail_screen.dart';
import 'portfolio_screen.dart';

class HomeShell extends StatefulWidget {
  final TradeGuardService service;
  const HomeShell({super.key, required this.service});

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  int _index = 0;

  @override
  Widget build(BuildContext context) {
    final screens = [
      ApprovalQueueScreen(service: widget.service),
      PortfolioScreen(service: widget.service),
      AuditTrailScreen(service: widget.service),
    ];

    return Scaffold(
      // IndexedStack, not screens[_index] — the earlier version swapped
      // which widget occupied this slot, which meant Flutter fully
      // destroyed and recreated the Queue screen on every tab switch,
      // tearing down its live PocketBase subscription each time. Since
      // that subscription is a broadcast stream (doesn't replay its
      // last value to a new listener), coming back to Queue meant
      // waiting indefinitely for the NEXT change — an infinite spinner
      // until nothing happened, or a hot restart forced everything
      // fresh. IndexedStack keeps all three screens permanently mounted
      // and just toggles which one paints, so the subscription (and any
      // in-flight Futures on the other tabs) survive tab switches.
      body: SafeArea(
        child: IndexedStack(
          index: _index,
          children: screens,
        ),
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (i) => setState(() => _index = i),
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.inbox_outlined),
            selectedIcon: Icon(Icons.inbox),
            label: 'Queue',
          ),
          NavigationDestination(
            icon: Icon(Icons.pie_chart_outline),
            selectedIcon: Icon(Icons.pie_chart),
            label: 'Portfolio',
          ),
          NavigationDestination(
            icon: Icon(Icons.receipt_long_outlined),
            selectedIcon: Icon(Icons.receipt_long),
            label: 'Audit',
          ),
        ],
      ),
    );
  }
}
