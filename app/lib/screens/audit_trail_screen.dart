import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../models/audit_entry.dart';
import '../services/pocketbase_service.dart';
import '../theme.dart';

class AuditTrailScreen extends StatelessWidget {
  final TradeGuardService service;
  const AuditTrailScreen({super.key, required this.service});

  @override
  Widget build(BuildContext context) {
    final dateFmt = DateFormat.Md().add_jm();

    // StreamBuilder instead of the old one-shot fetch-in-initState — same
    // fix, same reason, as the Portfolio screen: HomeShell's IndexedStack
    // keeps this screen permanently mounted, so a Future fetched once in
    // initState never refreshed after the first load. This screen no
    // longer needs to be a StatefulWidget at all now that there's no
    // local Future/state to hold — watchAuditLog() on the service is the
    // single source of truth, same shape as the Queue screen's build.
    return Scaffold(
      appBar: AppBar(title: const Text('Audit Trail')),
      body: StreamBuilder<List<AuditEntry>>(
        stream: service.watchAuditLog(),
        builder: (context, snapshot) {
          if (!snapshot.hasData) {
            return const Center(child: CircularProgressIndicator());
          }
          final entries = snapshot.data!;
          if (entries.isEmpty) {
            return ListView(
              children: const [
                Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'No decisions recorded yet. Every approval, denial, and '
                    'execution will show up here with full reasoning — this '
                    'is the explainability trail for judges or an auditor.',
                  ),
                ),
              ],
            );
          }
          return ListView.builder(
            padding: const EdgeInsets.all(12),
            itemCount: entries.length,
            itemBuilder: (context, i) {
              final e = entries[i];
              final color = switch (e.decision) {
                'approved' || 'executed' => AppTheme.buy,
                'denied' || 'failed' => AppTheme.sell,
                _ => AppTheme.warn,
              };
              return Card(
                margin: const EdgeInsets.only(bottom: 8),
                child: ListTile(
                  leading: CircleAvatar(
                    backgroundColor: color.withOpacity(0.15),
                    child: Icon(Icons.circle, color: color, size: 12),
                  ),
                  title: Text('${e.decision.toUpperCase()} · ${e.decidedBy}'),
                  subtitle: Text(e.notes.isEmpty ? 'Trade ${e.tradeId}' : e.notes),
                  trailing: Text(dateFmt.format(e.decidedAt)),
                ),
              );
            },
          );
        },
      ),
    );
  }
}
