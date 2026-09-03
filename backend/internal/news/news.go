// Package news supplies macro-calendar context, not decisions — the
// events here are attached to a proposed trade so the human approving
// it has the full picture. Nothing in this package feeds an autonomous
// trading decision. See the "why news is scoped this way" discussion in
// the architecture doc.
//
// Headline fetching lives in internal/mcpclient.Client.News, not here —
// it goes through the same real, verified MCP path as account and quote
// data, rather than a separate stubbed provider. This file used to have
// a Provider interface + StubProvider for that; removed once the real
// get_news tool was wired up, to avoid two competing "sources of news"
// in the codebase.
package news

import (
	"time"

	"github.com/yourusername/tradeguard/internal/models"
)

// HardcodedCalendar returns the known macro events for the hackathon
// build week (Aug 28 – Sept 4, 2026).
//
// VERIFIED against bls.gov, not guessed: the August 2026 jobs report
// (Employment Situation) releases Friday, September 4, 2026 at 8:30am
// ET — the last day of the build week. That's a genuinely good event
// for the demo: propose a trade near that time and watch the circuit
// breaker flag it.
//
// The next FOMC meeting is Sept 15–16, 2026 (per federalreserve.gov) —
// safely after the hackathon, so it's deliberately not included here.
// If you add more events later, double-check them against bls.gov or
// federalreserve.gov directly rather than trusting a general web search
// — release schedules shift.
func HardcodedCalendar() []models.MacroEvent {
	return []models.MacroEvent{
		{
			Name: "August jobs report (Employment Situation)",
			Time: mustParse("2026-09-04T12:30:00Z"), // 8:30am ET = 12:30 UTC
		},
	}
}

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
