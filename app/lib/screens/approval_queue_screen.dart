import 'package:flutter/material.dart';

import '../models/proposed_trade.dart';
import '../services/pocketbase_service.dart';
import '../theme.dart';
import '../widgets/trade_card.dart';

class ApprovalQueueScreen extends StatelessWidget {
  final TradeGuardService service;
  const ApprovalQueueScreen({super.key, required this.service});

  @override
  Widget build(BuildContext context) {
    return StreamBuilder<bool>(
      stream: service.watchPaused(),
      builder: (context, pausedSnapshot) {
        final paused = pausedSnapshot.data ?? false;

        return Scaffold(
          appBar: AppBar(
            title: const Text('Approval Queue'),
            actions: [
              // The kill switch. Pure PocketBase write (service.setPaused) —
              // both the agent and executor read this same record directly,
              // no Go backend round trip needed for this to take effect.
              IconButton(
                tooltip: paused ? 'Resume trading' : 'Pause trading',
                icon: Icon(paused ? Icons.play_circle_outline : Icons.pause_circle_outline),
                color: paused ? AppTheme.buy : null,
                onPressed: () => _confirmToggle(context, paused),
              ),
            ],
          ),
          body: Column(
            children: [
              if (paused)
                Container(
                  width: double.infinity,
                  color: AppTheme.warn.withOpacity(0.15),
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                  child: Row(
                    children: [
                      const Icon(Icons.pause_circle_filled, color: AppTheme.warn, size: 18),
                      const SizedBox(width: 8),
                      const Expanded(
                        child: Text(
                          'Trading paused — the agent won\'t propose new trades, and approved trades won\'t execute until resumed.',
                          style: TextStyle(color: AppTheme.warn, fontSize: 13),
                        ),
                      ),
                    ],
                  ),
                ),
              Expanded(
                child: StreamBuilder<List<ProposedTrade>>(
                  stream: service.watchPendingTrades(),
                  builder: (context, snapshot) {
                    if (!snapshot.hasData) {
                      return const Center(child: CircularProgressIndicator());
                    }
                    final trades = snapshot.data!;
                    if (trades.isEmpty) {
                      return const Center(
                        child: Padding(
                          padding: EdgeInsets.all(24),
                          child: Text(
                            'No pending proposals. New ones from the agent will appear here in real time — nothing executes until you approve it.',
                            textAlign: TextAlign.center,
                          ),
                        ),
                      );
                    }
                    return ListView.builder(
                      padding: const EdgeInsets.all(12),
                      itemCount: trades.length,
                      itemBuilder: (context, i) {
                        final trade = trades[i];
                        return TradeCard(
                          trade: trade,
                          onApprove: () => service.approve(trade),
                          onDeny: () => _showDenyDialog(context, trade),
                        );
                      },
                    );
                  },
                ),
              ),
            ],
          ),
        );
      },
    );
  }

  Future<void> _confirmToggle(BuildContext context, bool currentlyPaused) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(currentlyPaused ? 'Resume trading?' : 'Pause trading?'),
        content: Text(
          currentlyPaused
              ? 'The agent will resume proposing trades, and any already-approved trades will execute on the next poll.'
              : 'The agent will stop proposing new trades, and any already-approved trades will wait rather than execute — nothing already approved gets cancelled or lost, it just waits.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(currentlyPaused ? 'Resume' : 'Pause'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await service.setPaused(!currentlyPaused);
    }
  }

  Future<void> _showDenyDialog(BuildContext context, ProposedTrade trade) async {
    final controller = TextEditingController();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Deny ${trade.isCall ? "CALL" : "PUT"} on ${trade.underlying.isNotEmpty ? trade.underlying : trade.symbol}?'),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(hintText: 'Optional reason'),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton.tonal(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Deny'),
          ),
        ],
      ),
    );

    if (confirmed == true) {
      await service.deny(trade, note: controller.text.trim());
    }
  }
}
