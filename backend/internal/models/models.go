package models

import (
	"fmt"
	"strings"
	"time"
)

// PBTime wraps time.Time to correctly parse PocketBase's actual date
// format. Go's time.Time unmarshals JSON strings using strict RFC3339
// (a literal "T" between date and time — "2006-01-02T15:04:05Z"), but
// PocketBase's real output uses a space instead:
// "2026-08-18 07:41:17.386Z". VERIFIED against a real error hit in
// production, not a guess — that mismatch broke the executor's entire
// decode of the approved-trades response, since one bad field fails the
// whole json.Unmarshal call.
type PBTime struct {
	time.Time
}

// PocketBase's own layout, confirmed from a real error message: space
// separator, millisecond precision, Z suffix (PocketBase stores
// everything in UTC).
const pbTimeLayout = "2006-01-02 15:04:05.000Z"

func (t *PBTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		return nil
	}
	parsed, err := time.Parse(pbTimeLayout, s)
	if err != nil {
		// Fall back to RFC3339 defensively, in case this field is ever
		// populated from somewhere other than PocketBase's own autodate
		// output.
		parsed, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("parse PocketBase time %q: %w", s, err)
		}
	}
	t.Time = parsed
	return nil
}

// Side is the direction of a proposed or executed trade.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// Status tracks a proposed trade through its human-in-the-loop lifecycle.
// This progression IS the product — every state transition gets written
// to the audit log.
type Status string

const (
	StatusPending  Status = "pending"  // agent proposed it, awaiting human decision
	StatusApproved Status = "approved" // human approved, queued for execution
	StatusDenied   Status = "denied"   // human denied, terminal
	StatusExecuted Status = "executed" // order placed with Alpaca
	StatusFailed   Status = "failed"   // execution attempted but Alpaca rejected it
)

// ProposedTrade is the central record of the whole system. The agent
// creates these; a human resolves them; the executor fulfills them.
// ProposedTrade is the central record of the whole system. The agent
// creates these; a human resolves them; the executor fulfills them.
//
// As of the options-trading addition, Symbol holds the actual OCC
// option contract symbol (e.g. "AAPL250919C00320000") that gets sent
// to Alpaca — NOT the underlying ticker. Underlying and
// ContractDescription exist specifically so nothing downstream (policy
// matching, the human reading the approval card) has to parse an OCC
// symbol to know what's actually being traded.
type ProposedTrade struct {
	ID                  string   `json:"id,omitempty"`
	Symbol              string   `json:"symbol"`               // OCC option symbol — what actually gets sent to Alpaca
	Underlying          string   `json:"underlying"`           // e.g. "AAPL" — used for macro-event matching and display
	ContractDescription string   `json:"contract_description"` // e.g. "AAPL $320.00 Call exp 2026-09-19" — built by Go (ParseOCCSymbol), not trusted to the LLM
	Side                Side     `json:"side"`
	Qty                 float64  `json:"qty"`          // number of CONTRACTS, not shares — see OptionMultiplier
	Reasoning           string   `json:"reasoning"`    // plain-language explanation from the LLM
	BullCase            string   `json:"bull_case"`    // brief — one or two sentences, see agent's system prompt
	BearCase            string   `json:"bear_case"`    // brief — same
	NewsContext         string   `json:"news_context"` // headlines/context attached at proposal time
	RiskFlags           []string `json:"risk_flags"`   // e.g. "near-FOMC", "exceeds-position-cap"
	Status              Status   `json:"status"`
	CreatedAt           PBTime   `json:"created,omitempty"`
}

// AuditLogEntry records every decision made on a ProposedTrade, approved
// or denied, for the explainability/audit trail screen in the Flutter app.
type AuditLogEntry struct {
	ID        string    `json:"id,omitempty"`
	TradeID   string    `json:"trade_id"`
	Decision  Status    `json:"decision"`
	DecidedBy string    `json:"decided_by"` // for the hackathon this is just "you"
	Notes     string    `json:"notes"`
	DecidedAt time.Time `json:"decided_at,omitempty"`
}

// MacroEvent is a scheduled event (FOMC, CPI, jobs report, earnings)
// used by the policy engine to flag proposals made near high-volatility
// windows. Hardcoded for the hackathon week rather than pulled from a
// calendar API — see internal/news/news.go.
type MacroEvent struct {
	Name    string    `json:"name"`
	Time    time.Time `json:"time"`
	Symbols []string  `json:"symbols"` // empty means "affects everything" (e.g. FOMC)
}

// PolicyConfig is the risk envelope the policy engine enforces before a
// proposal ever reaches the human. It doesn't block trades on its own —
// it flags them, so the human always sees why something looks risky.
type PolicyConfig struct {
	MaxPositionSizeUSD    float64
	DailyLossLimitUSD     float64
	MacroEventBufferHours int
}

// AccountSnapshot is a periodic point-in-time record of paper account
// state, written by the agent loop and read by the Flutter portfolio
// screen's equity card. Field names (json tags) match
// tradeguard_app/lib/models/account_snapshot.dart exactly — don't rename
// one side without the other.
//
// Each RunOnce pass writes a NEW record rather than updating one in
// place (the Flutter side already sorts by -updated and takes the
// first, so this needs no client-side changes). Deliberate: at a
// 15-minute default interval, a week of snapshots is a few hundred
// records — trivial for PocketBase — and it's a free equity history if
// you ever want to chart it later instead of just showing the latest
// number.
type AccountSnapshot struct {
	ID          string  `json:"id,omitempty"`
	Equity      float64 `json:"equity"`
	BuyingPower float64 `json:"buying_power"`
	DayPnL      float64 `json:"day_pnl"` // signed — positive is a gain, negative is a loss
}

// SystemConfig is a single-record collection holding the kill switch.
// There's exactly one record; the agent and executor both read it, and
// the Flutter app writes to it directly (public update rule, same
// pattern as approving/denying a trade) — no Go involvement needed to
// flip it, matching how the rest of this app's human actions work.
type SystemConfig struct {
	ID     string `json:"id,omitempty"`
	Paused bool   `json:"paused"`
}
