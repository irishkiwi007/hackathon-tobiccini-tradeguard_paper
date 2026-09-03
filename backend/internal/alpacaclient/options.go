package alpacaclient

import (
	"fmt"
	"strconv"
	"time"
)

// IsOptionSymbol reports whether a symbol looks like a standard OCC
// option symbol (e.g. "AAPL250620C00100000") rather than a stock/ETF
// ticker or a crypto pair. Format: 1-6 char root symbol + YYMMDD (6
// digits) + C or P + strike*1000 zero-padded to 8 digits — 15
// characters of fixed suffix after the root symbol, always.
func IsOptionSymbol(symbol string) bool {
	if len(symbol) < 16 { // shortest possible: 1-char root + 15-char suffix
		return false
	}
	suffixStart := len(symbol) - 15
	root := symbol[:suffixStart]
	if root == "" {
		return false
	}
	for _, r := range root {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	cpChar := symbol[suffixStart+6]
	if cpChar != 'C' && cpChar != 'P' {
		return false
	}
	dateStr := symbol[suffixStart : suffixStart+6]
	if _, err := time.Parse("060102", dateStr); err != nil {
		return false
	}
	strikeStr := symbol[suffixStart+7:]
	if _, err := strconv.Atoi(strikeStr); err != nil {
		return false
	}
	return true
}

// ParsedOption is the decoded form of an OCC option symbol.
type ParsedOption struct {
	Underlying string
	Expiration time.Time
	Type       string // "call" or "put"
	Strike     float64
}

// ParseOCCSymbol decodes a standard OCC option symbol. Deliberately
// done in Go rather than trusted to the LLM — this is mechanical string
// parsing, not a judgment call, and getting it wrong would mean placing
// an order for the wrong contract entirely.
func ParseOCCSymbol(occ string) (ParsedOption, error) {
	if !IsOptionSymbol(occ) {
		return ParsedOption{}, fmt.Errorf("not a valid OCC option symbol: %q", occ)
	}
	suffixStart := len(occ) - 15
	root := occ[:suffixStart]
	dateStr := occ[suffixStart : suffixStart+6]
	cpChar := occ[suffixStart+6]
	strikeStr := occ[suffixStart+7:]

	exp, err := time.Parse("060102", dateStr)
	if err != nil {
		return ParsedOption{}, fmt.Errorf("parse OCC expiration %q: %w", dateStr, err)
	}

	strikeThousandths, err := strconv.Atoi(strikeStr)
	if err != nil {
		return ParsedOption{}, fmt.Errorf("parse OCC strike %q: %w", strikeStr, err)
	}

	optType := "call"
	if cpChar == 'P' {
		optType = "put"
	}

	return ParsedOption{
		Underlying: root,
		Expiration: exp,
		Type:       optType,
		Strike:     float64(strikeThousandths) / 1000.0,
	}, nil
}

// Describe renders a parsed option as a short, human-readable string
// for display and logging — e.g. "AAPL $320.00 Call exp 2026-09-19".
func (p ParsedOption) Describe() string {
	label := "Call"
	if p.Type == "put" {
		label = "Put"
	}
	return fmt.Sprintf("%s $%.2f %s exp %s", p.Underlying, p.Strike, label, p.Expiration.Format("2006-01-02"))
}

// OptionMultiplier is the number of underlying shares one standard
// option contract controls. Alpaca confirms this is always 100 for
// standard contracts (non-adjusted) — verified against the
// OptionContract schema, not assumed.
const OptionMultiplier = 100
