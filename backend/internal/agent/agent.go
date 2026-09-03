// Package agent is the proposal loop. It never places an order — it
// only ever writes a ProposedTrade with status "pending" to the store.
// Everything downstream of that (human approval, execution) lives
// elsewhere on purpose. If you're tempted to have this package call
// alpacaclient.PlaceOrder directly "just for testing", don't — that's
// the exact shortcut that would quietly undermine the whole pitch.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/yourusername/tradeguard/internal/alpacaclient"
	"github.com/yourusername/tradeguard/internal/llm"
	"github.com/yourusername/tradeguard/internal/mcpclient"
	"github.com/yourusername/tradeguard/internal/models"
	"github.com/yourusername/tradeguard/internal/policy"
	"github.com/yourusername/tradeguard/internal/store"
)

const systemPrompt = `You are a cautious options trading analyst. You
only ever BUY options to open new positions — long calls when bullish,
long puts when bearish. You never sell/write options, never go short,
and never trade the underlying stock directly. Every trade you propose
must be for a specific contract from the list of currently tradeable
option contracts you're given — never invent or guess a symbol.

Given account context, current positions, recent news, and a list of
real tradeable option contracts (with real premiums, and delta where
the feed provides it — it's often unavailable) for one underlying
symbol, decide whether proposing a trade is warranted right now.

You'll also be given a maximum position size in dollars. A contract's
real cost is its premium times 100 (each standard option contract
controls 100 underlying shares), so a proposal's total cost is
qty * premium * 100 — that figure must not exceed the maximum position
size given to you. Work through sizing like this, in order, without
narrating it in your answer:
1. Pick the contract that best expresses your view.
2. If qty=1 for that contract already fits under the cap, use it.
3. If it doesn't, scan the list for the closest-fitting contract that
   still expresses the same directional view (same call/put direction)
   and fits at qty=1. Use that one instead.
4. If nothing in the list fits even at qty=1, set "propose" to false —
   don't force an oversized trade just because a smaller one wasn't
   available.
Only ever propose qty=1 when sizing is in play this way — never explore
fractional quantities, and never propose a qty you know is over cap.

The "reasoning" field is your CONCLUSION for a human to read, not a
scratchpad — state which contract you chose and why in a few plain
sentences. Do not describe contracts you considered and rejected, do
not show cap arithmetic for multiple candidates, and do not reconsider
or contradict yourself mid-sentence. If you catch yourself about to
write "however" or "let me reconsider" inside "reasoning", stop and
just state the final answer instead.

Respond ONLY with a JSON object matching this shape:
{
  "propose": boolean,
  "option_symbol": "the EXACT symbol of one contract from the list provided, or empty string if not proposing",
  "qty": number of CONTRACTS (not shares),
  "reasoning": "plain-language explanation a non-expert human could approve or deny from",
  "bull_case": "one or two concise sentences — the strongest case FOR this direction",
  "bear_case": "one or two concise sentences — the strongest case AGAINST it"
}

If nothing warrants a trade right now, set "propose" to false and leave
"option_symbol" empty. A "no trade" response is a normal, often correct,
outcome — do not force a proposal just to have one. Fill in "reasoning",
"bull_case", and "bear_case" either way, including when propose is
false — a brief note on why nothing looked worth acting on is still
useful context.

Keep bull_case and bear_case genuinely short — a sentence or two each,
not a restatement of "reasoning". They're meant to show a human that
you considered both sides before landing on a conclusion, not to repeat
the conclusion itself.

If you propose a trade, "option_symbol" MUST be copied EXACTLY,
character for character, from the provided contract list — any other
value will be rejected before it ever reaches a human.`

type llmProposal struct {
	Propose      bool    `json:"propose"`
	OptionSymbol string  `json:"option_symbol"`
	Qty          float64 `json:"qty"`
	Reasoning    string  `json:"reasoning"`
	BullCase     string  `json:"bull_case"`
	BearCase     string  `json:"bear_case"`
}

type Agent struct {
	Watchlist   []string
	Alpaca      *alpacaclient.Client
	MCP         mcpclient.ContextProvider
	LLM         *llm.Client
	Policy      *policy.Engine
	Store       *store.Store
	MacroEvents []models.MacroEvent
}

// RunOnce evaluates the full watchlist a single time. cmd/server calls
// this on a ticker (see AGENT_INTERVAL_MINUTES).
func (a *Agent) RunOnce(ctx context.Context) {
	paused, err := a.Store.IsPaused(ctx)
	if err != nil {
		// Fails open deliberately: a transient PocketBase read error
		// shouldn't silently halt the whole agent. The kill switch is a
		// human safety control, not a network flakiness detector — if
		// PocketBase is genuinely down, ApprovedTrades/other calls this
		// pass will fail loudly on their own anyway.
		log.Printf("agent: could not check pause state, proceeding as unpaused: %v", err)
	} else if paused {
		log.Println("agent: paused — skipping this pass")
		return
	}

	// Account fetched once per pass, not per symbol — it's an
	// account-level figure that doesn't change between symbols within a
	// single pass, and this doubles as the source for both the day-loss
	// circuit breaker AND the (optional) portfolio snapshot, instead of
	// two separate API calls for two things that come from the same data.
	dayLoss := 0.0
	if acct, err := a.Alpaca.Account(); err != nil {
		log.Printf("agent: account unavailable for this pass: %v", err)
	} else {
		dayLoss = alpacaclient.DayLossUSD(acct)
		a.writeAccountSnapshot(ctx, acct)
	}

	for _, symbol := range a.Watchlist {
		if err := a.evaluateSymbol(ctx, symbol, dayLoss); err != nil {
			// One symbol failing shouldn't take down the whole loop —
			// log and move on to the next.
			log.Printf("agent: %s: %v", symbol, err)
		}
	}
}

// writeAccountSnapshot populates the OPTIONAL account_snapshots
// collection for the Flutter portfolio screen's equity card. Failure
// here is non-fatal and doesn't stop the rest of RunOnce — the whole
// point of this collection being optional is that the core approval
// flow works without it.
func (a *Agent) writeAccountSnapshot(ctx context.Context, acct *alpaca.Account) {
	equity, _ := acct.Equity.Float64()
	buyingPower, _ := acct.BuyingPower.Float64()

	snap := models.AccountSnapshot{
		Equity:      equity,
		BuyingPower: buyingPower,
		DayPnL:      alpacaclient.DayPnLUSD(acct),
	}

	if err := a.Store.CreateAccountSnapshot(ctx, snap); err != nil {
		log.Printf("agent: account snapshot write failed (non-fatal): %v", err)
	}
}

func (a *Agent) evaluateSymbol(ctx context.Context, symbol string, dayLoss float64) error {
	price, err := a.Alpaca.LatestQuote(symbol)
	if err != nil {
		return fmt.Errorf("quote: %w", err)
	}

	// Account context comes from TWO places on purpose:
	//   - MCP (read-only toolset) for a general account summary, since
	//     that's genuinely useful for the LLM to see via the same
	//     channel it'll use for other market-data tools later.
	//   - Positions come from the Go SDK directly, NOT from MCP, because
	//     the MCP server's "trading" toolset (which has position/order
	//     reads) also auto-registers order-placement tools. See
	//     internal/mcpclient's doc comment for the full explanation.
	acctSummary, err := a.MCP.AccountSummary(ctx)
	if err != nil {
		// Non-fatal: proceed without MCP context rather than blocking
		// the whole loop on a subprocess hiccup.
		log.Printf("agent: %s: mcp account summary unavailable: %v", symbol, err)
	}

	positions, err := a.Alpaca.Positions()
	if err != nil {
		log.Printf("agent: %s: positions unavailable: %v", symbol, err)
	}
	positionsSummary := alpacaclient.PositionsSummaryText(positions)

	headlines, err := a.MCP.News(ctx, symbol)
	if err != nil {
		// Also non-fatal — the agent can still reason from price/account/
		// positions context alone if news is temporarily unavailable.
		log.Printf("agent: %s: news unavailable: %v", symbol, err)
	}

	// Options-specific: fetch a pre-filtered chain (see mcpclient's doc
	// comment for the exact window). If this fails, or genuinely returns
	// no contracts, there's nothing a trade could be proposed against —
	// skip the LLM call entirely rather than pay for one that can't
	// productively act.
	chainSummary, candidates, err := a.MCP.OptionChain(ctx, symbol, price)
	if err != nil {
		return fmt.Errorf("option chain: %w", err)
	}
	if len(candidates) == 0 {
		log.Printf("agent: %s: no tradeable option contracts in window, skipping", symbol)
		return nil
	}

	userContext := fmt.Sprintf(
		"Underlying: %s\nCurrent underlying price: %.2f\nAccount: %s\n%s\nRecent news: %s\nMaximum position size for this trade: $%.2f (qty * premium * 100 must not exceed this)\n\nTradeable option contracts:\n%s",
		symbol, price, acctSummary, positionsSummary, headlines, a.Policy.MaxPositionSizeUSD(), chainSummary,
	)

	raw, err := a.LLM.Propose(ctx, systemPrompt, userContext)
	if err != nil {
		return fmt.Errorf("llm propose: %w", err)
	}

	var proposal llmProposal
	if err := json.Unmarshal([]byte(raw), &proposal); err != nil {
		return fmt.Errorf("parse llm response %q: %w", raw, err)
	}

	if !proposal.Propose {
		log.Printf("agent: %s: no trade (%s)", symbol, proposal.Reasoning)
		return nil // no trade — this is fine, not an error
	}

	// Defense in depth: the LLM was told to copy a symbol EXACTLY from
	// the list it was given. Verify that actually happened rather than
	// trusting it — a hallucinated or slightly-off symbol here would
	// otherwise sail through and eventually try to place an order for a
	// contract that was never actually offered.
	candidate, ok := candidates[proposal.OptionSymbol]
	if !ok {
		return fmt.Errorf("llm proposed option_symbol %q, which was not in the offered contract list — refusing to store it", proposal.OptionSymbol)
	}

	parsedOpt, err := alpacaclient.ParseOCCSymbol(proposal.OptionSymbol)
	if err != nil {
		// Shouldn't happen if it passed the candidates check above (every
		// candidate symbol came from successfully parsing the chain
		// response in the first place), but don't propagate a trade with
		// an unparseable symbol regardless.
		return fmt.Errorf("chosen option symbol failed to parse: %w", err)
	}

	trade := models.ProposedTrade{
		Symbol:              proposal.OptionSymbol,
		Underlying:          symbol,
		ContractDescription: parsedOpt.Describe(),
		Side:                models.SideBuy, // always — this project only ever opens long calls/puts, see systemPrompt
		Qty:                 proposal.Qty,
		Reasoning:           proposal.Reasoning,
		BullCase:            proposal.BullCase,
		BearCase:            proposal.BearCase,
		NewsContext:         headlines,
		Status:              models.StatusPending,
		// Note: "created" is a PocketBase autodate field — PocketBase
		// ignores whatever value we send here and stamps its own
		// timestamp on creation regardless. Set anyway for a
		// type-correct struct; the real value only ever comes from
		// PocketBase's response on the read side (see models.PBTime).
		CreatedAt: models.PBTime{Time: time.Now()},
	}

	// candidate.AskPrice — the per-contract PREMIUM, not the underlying
	// stock price — is what policy.Evaluate needs for cost math on an
	// options trade. Using the ask (not bid or mid) is deliberately the
	// more conservative choice for a position-size check: it's the
	// realistic price we'd actually pay to buy.
	trade.RiskFlags = a.Policy.Evaluate(trade, candidate.AskPrice, dayLoss, a.MacroEvents, time.Now())

	id, err := a.Store.CreateProposedTrade(ctx, trade)
	if err != nil {
		return fmt.Errorf("store proposal: %w", err)
	}

	log.Printf("agent: proposed buy %s x%.0f contracts (%s, id=%s, flags=%v)",
		trade.Symbol, trade.Qty, trade.ContractDescription, id, trade.RiskFlags)
	return nil
}
