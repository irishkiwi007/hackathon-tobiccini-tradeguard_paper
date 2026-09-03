import 'package:pocketbase/pocketbase.dart';

class ProposedTrade {
  final String id;
  final String symbol; // OCC option symbol — what actually got sent to Alpaca
  final String underlying; // e.g. "AAPL"
  final String contractDescription; // e.g. "AAPL $320.00 Call exp 2026-09-19" — built by the Go backend, not the LLM
  final String side; // always "buy" now — this project only opens long calls/puts
  final double qty; // number of CONTRACTS, not shares
  final String reasoning;
  final String bullCase;
  final String bearCase;
  final String newsContext;
  final List<String> riskFlags;
  final String status; // pending / approved / denied / executed / failed
  final DateTime created;

  ProposedTrade({
    required this.id,
    required this.symbol,
    required this.underlying,
    required this.contractDescription,
    required this.side,
    required this.qty,
    required this.reasoning,
    required this.bullCase,
    required this.bearCase,
    required this.newsContext,
    required this.riskFlags,
    required this.status,
    required this.created,
  });

  factory ProposedTrade.fromRecord(RecordModel r) {
    final data = r.data;
    return ProposedTrade(
      id: r.id,
      symbol: (data['symbol'] ?? '') as String,
      underlying: (data['underlying'] ?? '') as String,
      contractDescription: (data['contract_description'] ?? '') as String,
      side: (data['side'] ?? '') as String,
      qty: _asDouble(data['qty']),
      reasoning: (data['reasoning'] ?? '') as String,
      bullCase: (data['bull_case'] ?? '') as String,
      bearCase: (data['bear_case'] ?? '') as String,
      newsContext: (data['news_context'] ?? '') as String,
      riskFlags: (data['risk_flags'] is List)
          ? List<String>.from(data['risk_flags'] as List)
          : const <String>[],
      status: (data['status'] ?? 'pending') as String,
      created: DateTime.tryParse(r.created) ?? DateTime.now(),
    );
  }

  bool get isBuy => side.toLowerCase() == 'buy';

  /// True if any risk flag is the position-size-cap flag. Matches by
  /// prefix against the same string the Go backend's policy.CapFlagPrefix
  /// generates (see backend/internal/policy/policy.go) — Dart can't
  /// import that Go constant directly, so this literal has to be kept in
  /// sync with it by hand if that prefix ever changes.
  bool get exceedsPositionCap =>
      riskFlags.any((f) => f.startsWith('exceeds position size cap'));

  /// Derived from contractDescription (e.g. "AAPL $320.00 Call exp
  /// 2026-09-19") rather than stored separately — one source of truth,
  /// built once by the Go backend's OCC parser.
  bool get isCall => contractDescription.contains('Call');

  /// True display label — the contract description if we have one
  /// (options trades), falling back to the raw symbol for any old
  /// pre-options trade still sitting in the queue/history.
  String get displaySymbol => contractDescription.isNotEmpty ? contractDescription : symbol;

  static double _asDouble(dynamic v) {
    if (v is num) return v.toDouble();
    if (v is String) return double.tryParse(v) ?? 0;
    return 0;
  }
}
