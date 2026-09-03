import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../models/account_snapshot.dart';
import '../models/proposed_trade.dart';
import '../services/pocketbase_service.dart';
import '../theme.dart';

class PortfolioScreen extends StatefulWidget {
  final TradeGuardService service;
  const PortfolioScreen({super.key, required this.service});

  @override
  State<PortfolioScreen> createState() => _PortfolioScreenState();
}

class _PortfolioScreenState extends State<PortfolioScreen> {
  @override
  Widget build(BuildContext context) {
    final currency = NumberFormat.simpleCurrency();
    final dateFmt = DateFormat.Md().add_jm();

    // StreamBuilder, not FutureBuilder — this screen used to fetch once
    // in initState and never again, which meant approving/denying a
    // trade in the Queue never showed up here until a hot restart forced
    // initState to rerun (HomeShell's IndexedStack keeps this screen
    // mounted permanently, same as the Queue screen, so it needs the
    // same live-stream treatment Queue already had). No RefreshIndicator
    // needed anymore either, for the same reason Queue doesn't have one.
    return Scaffold(
      appBar: AppBar(title: const Text('Portfolio')),
      body: ListView(
        padding: const EdgeInsets.all(12),
        children: [
          StreamBuilder<AccountSnapshot?>(
            stream: widget.service.watchLatestSnapshot(),
            builder: (context, snapshot) {
              final s = snapshot.data;
              return Card(
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: s == null
                      ? const Text(
                          'No account snapshot yet. Wire the Go backend to write '
                          'to the account_snapshots collection to populate this '
                          'card (see README) — the activity feed below already '
                          'works without it.',
                        )
                      : Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('Equity', style: Theme.of(context).textTheme.labelMedium),
                            Text(
                              currency.format(s.equity),
                              style: Theme.of(context).textTheme.headlineSmall,
                            ),
                            const SizedBox(height: 8),
                            Row(
                              mainAxisAlignment: MainAxisAlignment.spaceBetween,
                              children: [
                                Text('Buying power: ${currency.format(s.buyingPower)}'),
                                Text(
                                  'Day P&L: ${currency.format(s.dayPnl)}',
                                  style: TextStyle(
                                    color: s.dayPnl >= 0 ? AppTheme.buy : AppTheme.sell,
                                    fontWeight: FontWeight.w600,
                                  ),
                                ),
                              ],
                            ),
                          ],
                        ),
                ),
              );
            },
          ),
          const SizedBox(height: 20),
          Text('Agent activity', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          StreamBuilder<List<ProposedTrade>>(
            stream: widget.service.watchActivityFeed(),
            builder: (context, snapshot) {
              if (!snapshot.hasData) {
                return const Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                );
              }
              final trades = snapshot.data!;
              if (trades.isEmpty) {
                return const Padding(
                  padding: EdgeInsets.all(16),
                  child: Text('No executed trades yet.'),
                );
              }
              return Column(
                children: trades.map((t) {
                  final color = t.status == 'executed' ? AppTheme.buy : AppTheme.sell;
                  final label = t.underlying.isNotEmpty ? t.underlying : t.symbol;
                  return Card(
                    margin: const EdgeInsets.only(bottom: 8),
                    child: ListTile(
                      leading: Icon(
                        t.isCall ? Icons.trending_up : Icons.trending_down,
                        color: color,
                      ),
                      title: Text(
                        '${t.isCall ? "CALL" : "PUT"} $label x${t.qty.toStringAsFixed(0)}',
                      ),
                      subtitle: Text(
                        t.contractDescription.isNotEmpty ? t.contractDescription : t.status,
                      ),
                      trailing: Text(dateFmt.format(t.created)),
                    ),
                  );
                }).toList(),
              );
            },
          ),
        ],
      ),
    );
  }
}
