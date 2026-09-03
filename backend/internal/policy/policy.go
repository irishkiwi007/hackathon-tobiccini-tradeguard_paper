// Package policy implements the risk envelope described in the
// architecture doc: position size caps, a daily loss limit, and a
// circuit breaker that flags (not silently blocks) trades proposed near
// scheduled macro events. Every flag it produces is meant to be shown to
// the human in the approval card, not hidden in a log somewhere.
package policy

import (
	"fmt"
	"time"

	"github.com/yourusername/tradeguard/internal/alpacaclient"
	"github.com/yourusername/tradeguard/internal/models"
)

type Engine struct {
	cfg models.PolicyConfig
}

func New(cfg models.PolicyConfig) *Engine {
	return &Engine{cfg: cfg}
}

// CapFlagPrefix is the fixed lead-in for the position-size-cap flag
// string below — exported so other packages (the executor's execution
// backstop, the Flutter app's Approve-button gate) can detect this
// specific flag by prefix without re-deriving the policy math. Keep this
// a literal prefix of the fmt.Sprintf in Evaluate below if either ever
// changes; the Flutter side hardcodes the same string separately since
// Dart can't import this constant.
const CapFlagPrefix = "exceeds position size cap"

// MaxPositionSizeUSD exposes the configured cap so callers outside this
// package can reference the exact number being enforced — e.g. the agent
// telling the LLM what it has to size proposals within, instead of the
// LLM finding out only after the fact via a flag on its own proposal.
func (e *Engine) MaxPositionSizeUSD() float64 {
	return e.cfg.MaxPositionSizeUSD
}

// Evaluate checks a proposed trade against the risk envelope and returns
// human-readable flags to attach to it. dailyRealizedLossUSD is whatever
// the caller has already tallied for today from executed trades.
//
// priceUSD means "per unit" — per share for a stock, or per-contract
// PREMIUM for an option. Options always control 100 underlying shares
// per standard contract (verified against Alpaca's OptionContract
// schema), so real dollar exposure for an option trade is
// qty * premium * 100, not qty * premium. Getting this wrong would
// silently let a position 100x past the intended cap through
// unflagged — this is checked via alpacaclient.IsOptionSymbol on the
// trade's actual OCC symbol, not assumed from context.
func (e *Engine) Evaluate(
	trade models.ProposedTrade,
	priceUSD float64,
	dailyRealizedLossUSD float64,
	macroEvents []models.MacroEvent,
	now time.Time,
) []string {
	var flags []string

	multiplier := 1.0
	if alpacaclient.IsOptionSymbol(trade.Symbol) {
		multiplier = alpacaclient.OptionMultiplier
	}
	notional := trade.Qty * priceUSD * multiplier
	if notional > e.cfg.MaxPositionSizeUSD {
		flags = append(flags, fmt.Sprintf(
			"%s ($%.2f > $%.2f)",
			CapFlagPrefix, notional, e.cfg.MaxPositionSizeUSD,
		))
	}

	if dailyRealizedLossUSD >= e.cfg.DailyLossLimitUSD {
		flags = append(flags, fmt.Sprintf(
			"daily loss limit reached ($%.2f >= $%.2f) — new trades should be treated with extra caution",
			dailyRealizedLossUSD, e.cfg.DailyLossLimitUSD,
		))
	}

	// Matched against Underlying, not Symbol — Symbol is now an OCC
	// option contract (e.g. "AAPL250919C00320000") for every trade this
	// project proposes, and a macro event's Symbols list (e.g. ["AAPL"])
	// would never match that directly.
	matchSymbol := trade.Underlying
	if matchSymbol == "" {
		matchSymbol = trade.Symbol // defensive fallback if Underlying wasn't set for some reason
	}

	buffer := time.Duration(e.cfg.MacroEventBufferHours) * time.Hour
	for _, ev := range macroEvents {
		if !affectsSymbol(ev, matchSymbol) {
			continue
		}
		diff := ev.Time.Sub(now)
		if diff > 0 && diff <= buffer {
			flags = append(flags, fmt.Sprintf(
				"%s scheduled in %s — expect volatility", ev.Name, diff.Round(time.Minute),
			))
		}
	}

	return flags
}

func affectsSymbol(ev models.MacroEvent, symbol string) bool {
	if len(ev.Symbols) == 0 {
		return true // e.g. FOMC affects everything
	}
	for _, s := range ev.Symbols {
		if s == symbol {
			return true
		}
	}
	return false
}
