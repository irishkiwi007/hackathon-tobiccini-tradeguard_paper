import 'package:pocketbase/pocketbase.dart';

class AuditEntry {
  final String id;
  final String tradeId;
  final String decision; // approved / denied / executed / failed
  final String decidedBy;
  final String notes;
  final DateTime decidedAt;

  AuditEntry({
    required this.id,
    required this.tradeId,
    required this.decision,
    required this.decidedBy,
    required this.notes,
    required this.decidedAt,
  });

  factory AuditEntry.fromRecord(RecordModel r) {
    final data = r.data;
    final decidedAtRaw = (data['decided_at'] ?? '') as String;
    return AuditEntry(
      id: r.id,
      tradeId: (data['trade_id'] ?? '') as String,
      decision: (data['decision'] ?? '') as String,
      decidedBy: (data['decided_by'] ?? '') as String,
      notes: (data['notes'] ?? '') as String,
      decidedAt: DateTime.tryParse(decidedAtRaw) ??
          DateTime.tryParse(r.created) ??
          DateTime.now(),
    );
  }
}
