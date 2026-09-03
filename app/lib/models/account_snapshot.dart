import 'package:pocketbase/pocketbase.dart';

/// Maps to an OPTIONAL `account_snapshots` collection. The core approval
/// flow doesn't depend on this — it's a nice-to-have for the portfolio
/// screen's equity card. See root README's "Known gaps" section: the Go
/// backend needs a small addition to write these periodically. Until
/// then, PortfolioScreen just shows an empty state instead of this data.
class AccountSnapshot {
  final double equity;
  final double buyingPower;
  final double dayPnl;
  final DateTime updated;

  AccountSnapshot({
    required this.equity,
    required this.buyingPower,
    required this.dayPnl,
    required this.updated,
  });

  factory AccountSnapshot.fromRecord(RecordModel r) {
    final data = r.data;
    return AccountSnapshot(
      equity: _asDouble(data['equity']),
      buyingPower: _asDouble(data['buying_power']),
      dayPnl: _asDouble(data['day_pnl']),
      updated: DateTime.tryParse(r.updated) ?? DateTime.now(),
    );
  }

  static double _asDouble(dynamic v) {
    if (v is num) return v.toDouble();
    if (v is String) return double.tryParse(v) ?? 0;
    return 0;
  }
}
