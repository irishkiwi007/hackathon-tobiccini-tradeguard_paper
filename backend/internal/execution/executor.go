// Package execution contains the ONLY code path in this project that
// places a real (paper) order. It acts strictly on trades a human has
// already approved in the Flutter app — it never evaluates or decides
// anything itself.
package execution

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/yourusername/tradeguard/internal/alpacaclient"
	"github.com/yourusername/tradeguard/internal/models"
	"github.com/yourusername/tradeguard/internal/policy"
	"github.com/yourusername/tradeguard/internal/store"
)

type Executor struct {
	Alpaca *alpacaclient.Client
	Store  *store.Store
}

// RunOnce polls for approved-but-not-yet-executed trades and executes
// each one. cmd/server calls this on a ticker (see EXECUTOR_POLL_SECONDS).
//
// NOTE: this is a polling design for simplicity under hackathon time
// pressure. A PocketBase realtime subscription (or a hook that calls
// back into this service on record update) would be a nice upgrade if
// time allows, but polling is easier to get right first.
func (e *Executor) RunOnce(ctx context.Context) {
	paused, err := e.Store.IsPaused(ctx)
	if err != nil {
		// Same fail-open reasoning as internal/agent.RunOnce — see that
		// comment. A transient read error shouldn't halt execution of
		// trades a human already approved.
		log.Printf("executor: could not check pause state, proceeding as unpaused: %v", err)
	} else if paused {
		// Deliberately does NOT touch anything here — approved trades
		// stay "approved" and simply get picked up on the next poll
		// once unpaused. Pausing must never silently drop or fail a
		// trade a human already signed off on.
		log.Println("executor: paused — skipping this poll")
		return
	}

	trades, err := e.Store.ApprovedTrades(ctx)
	if err != nil {
		log.Printf("executor: fetch approved trades: %v", err)
		return
	}

	for _, t := range trades {
		if err := e.execute(ctx, t); err != nil {
			log.Printf("executor: %s: %v", t.Symbol, err)
			_ = e.Store.UpdateTradeStatus(ctx, t.ID, models.StatusFailed)
			continue
		}
	}
}

func (e *Executor) execute(ctx context.Context, t models.ProposedTrade) error {
	// Backstop, not a duplicate of the Flutter-side gate: a trade could
	// reach "approved" without ever passing through the app's Approve
	// button (a stale client, a direct PocketBase write, a future
	// integration) so this check has to stand on its own rather than
	// assume the UI already caught it.
	if capExceeded(t.RiskFlags) {
		return e.fail(ctx, t, "refused — exceeds position size cap: "+strings.Join(t.RiskFlags, "; "))
	}

	orderID, err := e.Alpaca.PlaceOrder(t)
	if err != nil {
		if isMarketClosedRejection(err) {
			// Transient, not a real failure: Alpaca won't accept options
			// market orders outside market hours. Leave the trade
			// "approved" instead of marking it Failed, so the next 10s
			// poll retries it automatically once the market opens — the
			// same wait-and-retry treatment RunOnce already gives paused
			// trades, and for the same reason: this isn't a decision
			// that needs explaining, it's a queued state. No audit entry
			// for the same reason paused trades don't get one either;
			// the log line is enough to see it happening without a new
			// audit_log row on every ~10s poll while the market's closed.
			log.Printf("executor: %s: market closed, will retry once it opens", t.Symbol)
			return nil
		}
		// Alpaca rejecting the order for any other reason (insufficient
		// buying power, an invalid contract, etc.) needs to reach the
		// audit trail exactly as much as a successful execution does —
		// this used to just return err and let RunOnce's generic
		// fallback set status to Failed with no audit_log entry at all,
		// so a real rejection was invisible on the Audit screen even
		// though Portfolio's activity feed showed a red "failed" chip
		// right next to it with nothing explaining why.
		return e.fail(ctx, t, "order rejected by Alpaca: "+err.Error())
	}

	if err := e.Store.UpdateTradeStatus(ctx, t.ID, models.StatusExecuted); err != nil {
		return err
	}

	log.Printf("executor: executed %s %s x%.4f -> order %s", t.Side, t.Symbol, t.Qty, orderID)

	return e.Store.CreateAuditEntry(ctx, models.AuditLogEntry{
		TradeID:   t.ID,
		Decision:  models.StatusExecuted,
		DecidedBy: "system:executor",
		Notes:     "order placed with Alpaca (order id " + orderID + ")",
		DecidedAt: time.Now(),
	})
}

// fail marks a trade Failed and records the reason in the audit trail in
// one place, so every non-execution outcome — a policy refusal, a
// rejected order, anything added here later — leaves the same trail a
// successful execution does. RunOnce's own fallback (set status, no
// audit entry) only fires now if fail() itself can't reach the store,
// which is a genuine infra problem rather than an explainable trade
// outcome.
func (e *Executor) fail(ctx context.Context, t models.ProposedTrade, reason string) error {
	log.Printf("executor: %s: %s", t.Symbol, reason)
	if err := e.Store.UpdateTradeStatus(ctx, t.ID, models.StatusFailed); err != nil {
		return err
	}
	return e.Store.CreateAuditEntry(ctx, models.AuditLogEntry{
		TradeID:   t.ID,
		Decision:  models.StatusFailed,
		DecidedBy: "system:executor",
		Notes:     reason,
		DecidedAt: time.Now(),
	})
}

// isMarketClosedRejection reports whether err is Alpaca rejecting an
// options order because the market is currently closed — a timing
// condition, not an invalid trade. Detected by substring against the
// error message (the exact text Alpaca returns, confirmed against a
// real rejection: "options market orders are only allowed during market
// hours") rather than a type assertion against the Alpaca SDK's error
// type, so this doesn't need to know that SDK's internal error shape.
func isMarketClosedRejection(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "market hours")
}

// capExceeded checks a trade's already-computed RiskFlags (set once by
// the policy engine at proposal time, in internal/agent) for the
// position-size-cap flag by prefix, rather than recomputing the notional
// here — that keeps this single-purpose check in sync with whatever
// policy.Evaluate actually decided, with no second copy of the cap math
// to drift out of step.
func capExceeded(flags []string) bool {
	for _, f := range flags {
		if strings.HasPrefix(f, policy.CapFlagPrefix) {
			return true
		}
	}
	return false
}
