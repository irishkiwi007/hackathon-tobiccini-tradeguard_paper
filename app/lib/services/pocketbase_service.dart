import 'dart:async';

import 'package:pocketbase/pocketbase.dart';

import '../config.dart';
import '../models/account_snapshot.dart';
import '../models/audit_entry.dart';
import '../models/proposed_trade.dart';

/// No auth in this client on purpose — the whole product's safety story
/// comes from the human approval + audit trail, not from access control
/// on a single-user hackathon demo app. For anything beyond the demo,
/// add PocketBase user auth and scope the collection API rules to it.
///
/// For this to work as-is, set the `proposed_trades` and `audit_log`
/// collections' List/View/Update/Create rules to "" (public) in the
/// PocketBase admin UI — see the README for the exact steps.
class TradeGuardService {
  final PocketBase pb;

  TradeGuardService() : pb = PocketBase(AppConfig.pocketbaseUrl) {
    // Belt-and-suspenders alongside the subscribe() calls below: some
    // hosting edges (confirmed on Railway, as of this fix) buffer or
    // outright drop the long-lived SSE connection PocketBase's realtime
    // API depends on, silently — no error, updates just never arrive
    // until something else (a hot restart, a manual refresh) forces a
    // fresh fetch. This timer re-fetches everything every few seconds
    // regardless of whether the SSE push actually worked, so a Railway
    // (or similarly SSE-unfriendly) deployment still self-corrects
    // quickly instead of silently going stale. Harmless where SSE does
    // work fine — it just becomes a redundant extra fetch.
    _pollTimer = Timer.periodic(const Duration(seconds: 4), (_) => _pollOnce());
  }

  late final Timer _pollTimer;

  Future<void> _pollOnce() async {
    // Each guarded independently so one failing (e.g. account_snapshots
    // not existing yet) never stops the others from refreshing.
    for (final refresh in [
      _refreshPending,
      _refreshPaused,
      _refreshActivity,
      _refreshAudit,
      _refreshSnapshot,
    ]) {
      try {
        await refresh();
      } catch (_) {}
    }
  }

  final _pendingController = StreamController<List<ProposedTrade>>.broadcast();
  bool _subscribed = false;

  /// Realtime stream of pending trades, newest first. Safe to listen to
  /// from multiple widgets — subscribes to PocketBase once and fans out
  /// to every listener.
  Stream<List<ProposedTrade>> watchPendingTrades() {
    if (!_subscribed) {
      _subscribed = true;
      _initPendingStream();
    }
    return _pendingController.stream;
  }

  Future<void> _initPendingStream() async {
    await _refreshPending();

    // Verified against the official pocketbase Dart SDK docs — this
    // call shape (collection-scoped subscribe with a '*' topic) is
    // correct as written.
    await pb.collection('proposed_trades').subscribe('*', (e) async {
      await _refreshPending();
    });
  }

  Future<void> _refreshPending() async {
    final records = await pb.collection('proposed_trades').getFullList(
          filter: "status = 'pending'",
          sort: '-created',
        );
    _pendingController.add(records.map(ProposedTrade.fromRecord).toList());
  }

  final _pausedController = StreamController<bool>.broadcast();
  bool _pausedSubscribed = false;
  String? _configRecordId; // cached so setPaused doesn't need to re-fetch it

  /// Realtime stream of the kill switch state. Same broadcast + single-
  /// subscribe pattern as watchPendingTrades — safe now that HomeShell
  /// uses IndexedStack and this screen never gets destroyed/recreated
  /// on tab switches (see that fix's commit history if this regresses).
  Stream<bool> watchPaused() {
    if (!_pausedSubscribed) {
      _pausedSubscribed = true;
      _initPausedStream();
    }
    return _pausedController.stream;
  }

  Future<void> _initPausedStream() async {
    await _refreshPaused();
    await pb.collection('system_config').subscribe('*', (e) async {
      await _refreshPaused();
    });
  }

  Future<void> _refreshPaused() async {
    try {
      final records = await pb.collection('system_config').getFullList();
      if (records.isEmpty) {
        _pausedController.add(false);
        return;
      }
      _configRecordId = records.first.id;
      _pausedController.add(records.first.data['paused'] == true);
    } catch (_) {
      // Collection missing (old backend, patch script not run yet) —
      // treat as "not paused" rather than crash the Queue screen over
      // an optional safety feature.
      _pausedController.add(false);
    }
  }

  /// Flips the kill switch. Pure PocketBase write, same as approve/deny
  /// — no Go backend involvement needed, both the agent and executor
  /// read this same record directly on their own next pass.
  Future<void> setPaused(bool paused) async {
    if (_configRecordId == null) {
      await _refreshPaused(); // make sure we have the record id first
    }
    if (_configRecordId == null) return; // collection genuinely doesn't exist yet
    await pb.collection('system_config').update(_configRecordId!, body: {
      'paused': paused,
    });
  }

  /// Approves a trade and writes the audit entry in one action. The Go
  /// executor picks this up on its next poll and turns it into a real
  /// (paper) order — this method never talks to Alpaca directly.
  Future<void> approve(ProposedTrade trade) async {
    await pb.collection('proposed_trades').update(trade.id, body: {
      'status': 'approved',
    });
    await pb.collection('audit_log').create(body: {
      'trade_id': trade.id,
      'decision': 'approved',
      'decided_by': 'you',
      'notes': '',
      'decided_at': DateTime.now().toUtc().toIso8601String(),
    });
  }

  Future<void> deny(ProposedTrade trade, {String note = ''}) async {
    await pb.collection('proposed_trades').update(trade.id, body: {
      'status': 'denied',
    });
    await pb.collection('audit_log').create(body: {
      'trade_id': trade.id,
      'decision': 'denied',
      'decided_by': 'you',
      'notes': note,
      'decided_at': DateTime.now().toUtc().toIso8601String(),
    });
  }

  /// Executed/failed trades, for the portfolio screen's activity feed.
  Future<List<ProposedTrade>> fetchActivityFeed() async {
    final records = await pb.collection('proposed_trades').getFullList(
          filter: "status = 'executed' || status = 'failed'",
          sort: '-created',
        );
    return records.map(ProposedTrade.fromRecord).toList();
  }

  final _activityController = StreamController<List<ProposedTrade>>.broadcast();
  bool _activitySubscribed = false;

  /// Realtime stream of executed/failed trades, for the portfolio
  /// screen's activity feed. Same broadcast + subscribe-once pattern as
  /// watchPendingTrades, and listens to the SAME `proposed_trades`
  /// collection — a proposal moving from pending to approved to
  /// executed is all the same record changing, so this is a second,
  /// independent `.subscribe('*', ...)` on that collection filtered to
  /// the opposite set of statuses, not a different topic. The Dart SDK
  /// supports multiple independent listeners per collection subscribe.
  ///
  /// This is what was missing before: fetchActivityFeed() above only
  /// ever ran once, from Portfolio's initState — and since HomeShell's
  /// IndexedStack (see that screen's doc comment) keeps Portfolio
  /// mounted for the app's whole lifetime, initState never ran again.
  /// Approving a trade updated PocketBase correctly; nothing told the
  /// already-mounted screen to go look. This stream fixes that the same
  /// way watchPendingTrades already fixed it for the Queue screen.
  Stream<List<ProposedTrade>> watchActivityFeed() {
    if (!_activitySubscribed) {
      _activitySubscribed = true;
      _initActivityStream();
    }
    return _activityController.stream;
  }

  Future<void> _initActivityStream() async {
    await _refreshActivity();
    await pb.collection('proposed_trades').subscribe('*', (e) async {
      await _refreshActivity();
    });
  }

  Future<void> _refreshActivity() async {
    _activityController.add(await fetchActivityFeed());
  }

  /// Every decision ever made — approvals, denials, executions — for the
  /// audit trail screen. This is the explainability showcase for judges.
  Future<List<AuditEntry>> fetchAuditLog() async {
    final records = await pb.collection('audit_log').getFullList(
          sort: '-decided_at',
        );
    return records.map(AuditEntry.fromRecord).toList();
  }

  final _auditController = StreamController<List<AuditEntry>>.broadcast();
  bool _auditSubscribed = false;

  /// Realtime stream of the audit trail. Same fix, same reason, as
  /// watchActivityFeed above — the Audit screen had the identical
  /// one-shot-fetch-in-initState problem, just against `audit_log`
  /// instead of `proposed_trades`.
  Stream<List<AuditEntry>> watchAuditLog() {
    if (!_auditSubscribed) {
      _auditSubscribed = true;
      _initAuditStream();
    }
    return _auditController.stream;
  }

  Future<void> _initAuditStream() async {
    await _refreshAudit();
    await pb.collection('audit_log').subscribe('*', (e) async {
      await _refreshAudit();
    });
  }

  Future<void> _refreshAudit() async {
    _auditController.add(await fetchAuditLog());
  }

  /// Optional — returns null gracefully if the `account_snapshots`
  /// collection doesn't exist yet or has no rows. See AccountSnapshot's
  /// doc comment for why this is separated from the core flow.
  Future<AccountSnapshot?> fetchLatestSnapshot() async {
    try {
      final records = await pb.collection('account_snapshots').getFullList(
            sort: '-updated',
          );
      if (records.isEmpty) return null;
      return AccountSnapshot.fromRecord(records.first);
    } catch (_) {
      return null;
    }
  }

  final _snapshotController = StreamController<AccountSnapshot?>.broadcast();
  bool _snapshotSubscribed = false;

  /// Realtime stream of the latest account snapshot, for Portfolio's
  /// equity card. Same fix as the two streams above. The subscribe call
  /// itself is wrapped separately from fetchLatestSnapshot's own
  /// try/catch, since subscribing to a collection that doesn't exist yet
  /// throws where a plain getFullList on it doesn't — this keeps the
  /// "collection not created yet" case as harmless here as it already
  /// was for the one-shot fetch.
  Stream<AccountSnapshot?> watchLatestSnapshot() {
    if (!_snapshotSubscribed) {
      _snapshotSubscribed = true;
      _initSnapshotStream();
    }
    return _snapshotController.stream;
  }

  Future<void> _initSnapshotStream() async {
    await _refreshSnapshot();
    try {
      await pb.collection('account_snapshots').subscribe('*', (e) async {
        await _refreshSnapshot();
      });
    } catch (_) {
      // Collection doesn't exist yet — same non-fatal handling as
      // fetchLatestSnapshot; the equity card just stays on its "wire the
      // Go backend" placeholder until the collection shows up.
    }
  }

  Future<void> _refreshSnapshot() async {
    _snapshotController.add(await fetchLatestSnapshot());
  }

  void dispose() {
    _pollTimer.cancel();
    pb.collection('proposed_trades').unsubscribe('*');
    pb.collection('system_config').unsubscribe('*');
    pb.collection('audit_log').unsubscribe('*');
    try {
      pb.collection('account_snapshots').unsubscribe('*');
    } catch (_) {}
    _pendingController.close();
    _pausedController.close();
    _activityController.close();
    _auditController.close();
    _snapshotController.close();
  }
}
