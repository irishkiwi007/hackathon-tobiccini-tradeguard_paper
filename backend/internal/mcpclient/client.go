// Package mcpclient talks to Alpaca's official MCP server
// (github.com/alpacahq/alpaca-mcp-server, Python/FastMCP v2) over stdio,
// using the mark3labs/mcp-go client library.
//
// IMPORTANT — VERIFIED FINDING, not a guess: the Alpaca MCP server ties
// its order-placement tools (place_stock_order, place_crypto_order,
// place_option_order) to the "trading" toolset. If ALPACA_TOOLSETS
// includes "trading" — even just to get read tools like getAllOrders or
// getAllOpenPositions — the server ALSO auto-registers the order
// placement tools (see alpacahq/alpaca-mcp-server's server.go,
// _register_trading_overrides). There is no config knob to get
// "trading" read access without also getting order placement.
//
// Because of that, this project deliberately does NOT include "trading"
// in ALPACA_TOOLSETS (see .env.example — default is
// "account,stock-data,options-data,news,assets"). Position/order
// awareness for the agent instead comes from
// internal/alpacaclient.Positions(), which hits Alpaca's Trading API
// directly via the Go SDK — a path we already trust for reads, and
// which has no order-placement capability wired to it either.
//
// As defense in depth on top of that config choice, CallTool below also
// hard-refuses any tool name matching a denylist, so a future config
// mistake (someone adding "trading" back to ALPACA_TOOLSETS without
// reading this comment) fails safe instead of silently giving the LLM
// an order-placement tool.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/yourusername/tradeguard/internal/alpacaclient"
)

// deniedToolSubstrings blocks any tool whose name contains one of these
// fragments, regardless of what ALPACA_TOOLSETS is configured to allow.
// Matches against the real tool names verified from the server source:
// place_stock_order, place_crypto_order, place_option_order,
// patchOrderByOrderId → update_order (etc.), deleteOrder → cancel_order.
var deniedToolSubstrings = []string{
	"order",  // covers place_*_order, cancel_order, update_order, etc.
	"delete", // covers deleteOpenPosition, deleteAllOpenPositions, etc.
}

// Config controls how the MCP server subprocess is launched.
type Config struct {
	APIKey     string // ALPACA_API_KEY
	SecretKey  string // ALPACA_SECRET_KEY
	PaperTrade bool   // ALPACA_PAPER_TRADE
	Toolsets   string // ALPACA_TOOLSETS — keep "trading" OUT of this, see above
}

// ContextProvider is what internal/agent depends on. Keeping it as an
// interface makes it trivial to swap in a mock for local dev, or to
// fall back to direct Alpaca REST calls for a demo if the MCP server
// misbehaves mid-hackathon.
//
// OptionCandidate is what the caller needs about one contract after
// the LLM has picked it — just enough to look up its actual premium
// for policy evaluation without re-parsing the whole chain response.
type OptionCandidate struct {
	AskPrice float64
	BidPrice float64
}

type ContextProvider interface {
	AccountSummary(ctx context.Context) (string, error)
	Quote(ctx context.Context, symbol string) (string, error)
	News(ctx context.Context, symbol string) (string, error)
	// OptionChain returns an LLM-readable summary of tradeable contracts
	// AND a map of every OCC symbol offered to its quote — so the
	// caller can both validate the LLM's eventual choice against real,
	// actually-offered contracts (not a hallucinated symbol) and look up
	// its actual premium for policy/cost evaluation, without re-parsing
	// the raw chain response a second time.
	OptionChain(ctx context.Context, underlying string, currentPrice float64) (summary string, candidates map[string]OptionCandidate, err error)
	Close() error
}

type Client struct {
	mcp *client.Client
}

// New launches the Alpaca MCP server as a subprocess (`alpaca-mcp-server`,
// installable via `pip install alpaca-mcp-server` or `uvx alpaca-mcp-server`
// — confirmed on PyPI) and performs the MCP initialize handshake.
func New(ctx context.Context, cfg Config) (*Client, error) {
	env := []string{
		"ALPACA_API_KEY=" + cfg.APIKey,
		"ALPACA_SECRET_KEY=" + cfg.SecretKey,
		fmt.Sprintf("ALPACA_PAPER_TRADE=%v", cfg.PaperTrade),
		"ALPACA_TOOLSETS=" + cfg.Toolsets,
	}

	mcpClient, err := client.NewStdioMCPClient("alpaca-mcp-server", env)
	if err != nil {
		return nil, fmt.Errorf("start alpaca-mcp-server subprocess: %w", err)
	}

	// CRITICAL: drain stderr immediately, unconditionally, for the life of
	// this client. This isn't optional logging — it's load-bearing.
	//
	// VERIFIED root cause of a real deadlock hit during development: if
	// nobody reads a subprocess's stderr pipe, the OS pipe buffer fills
	// once the Python server writes enough to it (FastMCP prints a sizeable
	// startup banner + log lines). Once full, the child's next stderr
	// write() call BLOCKS — which stalls the entire Python process,
	// including whatever tool call was about to finish and write its
	// response to stdout. From this Go program's side that looked exactly
	// like the server hanging forever on the first real tool call, even
	// though the actual cause was entirely on our side: an unread pipe.
	// Don't remove this without understanding that history.
	if stderr, ok := client.GetStderr(mcpClient); ok {
		go func() {
			buf := make([]byte, 4096)
			for {
				n, readErr := stderr.Read(buf)
				if n > 0 {
					log.Printf("[alpaca-mcp-server] %s", string(buf[:n]))
				}
				if readErr != nil {
					return
				}
			}
		}()
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "tradeguard-agent",
		Version: "0.1.0",
	}

	if _, err := mcpClient.Initialize(ctx, initReq); err != nil {
		_ = mcpClient.Close()
		return nil, fmt.Errorf("mcp initialize handshake: %w", err)
	}

	return &Client{mcp: mcpClient}, nil
}

// AccountSummary calls the get_account_info tool for a plain-language
// account snapshot (buying power, equity, status) to give the LLM
// context before it reasons about a new trade.
func (c *Client) AccountSummary(ctx context.Context) (string, error) {
	return c.callTool(ctx, "get_account_info", nil)
}

// Quote calls the get_stock_latest_quote tool for current bid/ask
// context on a symbol. Verified against the server's OpenAPI spec: the
// argument is "symbols" (plural), a comma-separated string, even for a
// single symbol.
func (c *Client) Quote(ctx context.Context, symbol string) (string, error) {
	return c.callTool(ctx, "get_stock_latest_quote", map[string]any{
		"symbols": symbol,
	})
}

// News calls the get_news tool and returns a short, clean summary of
// headlines — NOT the raw tool output. That raw output is substantial:
// Alpaca's own prompt-injection security envelope (meant for an LLM to
// read as instructions, not a human to read in an approval card) plus
// full per-article JSON (author, multiple image URLs per size, source
// object, pagination token, etc.). Confirmed this was leaking straight
// into the Flutter approval card as an unreadable wall of raw JSON
// before this fix — this method now does the parsing so nothing
// upstream (the LLM prompt, the human-facing card) ever sees that raw
// shape.
//
// Verified against the server's OpenAPI spec: "symbols" (plural,
// comma-separated) and "limit" are the relevant arguments; several
// other filters exist (date range, content inclusion) but aren't
// needed here.
func (c *Client) News(ctx context.Context, symbol string) (string, error) {
	raw, err := c.callTool(ctx, "get_news", map[string]any{
		"symbols": symbol,
		"limit":   5,
	})
	if err != nil {
		return "", err
	}
	return summarizeNews(raw), nil
}

// OptionChain calls get_option_chain, pre-filtered by OUR code — not
// left to the LLM to figure out filter arguments — to a manageable
// window: expirations roughly 25-45 days out (long enough that theta
// decay isn't dominant day to day, short enough to stay liquid) and
// strikes within ~12% of the current underlying price (both calls and
// puts, so the LLM can express either direction). Verified against the
// server's OpenAPI spec: strike_price_gte/lte are numbers,
// expiration_date_gte/lte are date strings, limit is an integer. Feed
// is deliberately left unset — Alpaca's own default is "opra" if
// subscribed, "indicative" (free) otherwise, which is exactly the
// right behavior for a paper account with no market data subscription.
func (c *Client) OptionChain(ctx context.Context, underlying string, currentPrice float64) (string, map[string]OptionCandidate, error) {
	now := time.Now()
	minExpiry := now.AddDate(0, 0, 25).Format("2006-01-02")
	maxExpiry := now.AddDate(0, 0, 45).Format("2006-01-02")
	minStrike := currentPrice * 0.88
	maxStrike := currentPrice * 1.12

	raw, err := c.callTool(ctx, "get_option_chain", map[string]any{
		"underlying_symbol":   underlying,
		"expiration_date_gte": minExpiry,
		"expiration_date_lte": maxExpiry,
		"strike_price_gte":    minStrike,
		"strike_price_lte":    maxStrike,
		"limit":               20,
	})
	if err != nil {
		return "", nil, err
	}
	return summarizeOptionChain(raw, underlying)
}

// OptionChainRaw is a DIAGNOSTIC method only — returns the exact,
// unparsed JSON text the tool call produced, with no summarization or
// struct parsing applied. Added specifically to answer one question:
// when option greeks/prices look wrong (e.g. delta always exactly
// 0.00), is that because our Go struct's field names don't match what
// the server actually sends (a parsing bug — data's fine, we're reading
// it wrong), or because the server itself is returning zeroed-out /
// placeholder values (a feed limitation, most likely tied to Alpaca's
// free "indicative" options feed, which its own docs describe as
// having "modified" quotes)? Not used by the agent — production code
// should always go through OptionChain, which validates and summarizes
// before anything reaches the LLM or a human.
func (c *Client) OptionChainRaw(ctx context.Context, underlying string, currentPrice float64) (string, error) {
	now := time.Now()
	minExpiry := now.AddDate(0, 0, 25).Format("2006-01-02")
	maxExpiry := now.AddDate(0, 0, 45).Format("2006-01-02")
	minStrike := currentPrice * 0.88
	maxStrike := currentPrice * 1.12

	return c.callTool(ctx, "get_option_chain", map[string]any{
		"underlying_symbol":   underlying,
		"expiration_date_gte": minExpiry,
		"expiration_date_lte": maxExpiry,
		"strike_price_gte":    minStrike,
		"strike_price_lte":    maxStrike,
		"limit":               3, // small on purpose — this is for reading by eye, not the agent's use
	})
}

type newsToolResponse struct {
	Data struct {
		News []struct {
			Headline  string `json:"headline"`
			Source    string `json:"source"`
			CreatedAt string `json:"created_at"`
		} `json:"news"`
	} `json:"data"`
}

// summarizeNews extracts just headline/source/date from the raw tool
// JSON, dropping the security envelope and every other field. Falls
// back to a short placeholder rather than the raw blob if parsing ever
// fails (e.g. the server changes its response shape) — a human should
// never see raw JSON in the approval card, even in a failure case.
func summarizeNews(raw string) string {
	var parsed newsToolResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.News) == 0 {
		return "No recent news found."
	}

	var lines []string
	for _, item := range parsed.Data.News {
		date := item.CreatedAt
		if t, err := time.Parse(time.RFC3339, item.CreatedAt); err == nil {
			date = t.Format("Jan 2")
		}
		lines = append(lines, fmt.Sprintf("- %s (%s, %s)", item.Headline, item.Source, date))
	}
	return strings.Join(lines, "\n")
}

// Response shape assumed consistent with every other tool on this
// server (get_account_info, get_stock_latest_quote, get_news all wrap
// their payload in {"_alpaca_mcp_security": ..., "data": {...}}) — not
// separately confirmed for this exact tool via a live call, but
// following the same pattern the rest of the server uses everywhere
// else. snapshots is a MAP keyed by OCC symbol, not an array — a
// different shape than news, confirmed from the OpenAPI response
// schema (option_snapshots_resp / option_quote).
type optionChainToolResponse struct {
	Data struct {
		Snapshots map[string]struct {
			Greeks *struct {
				Delta float64 `json:"delta"`
			} `json:"greeks"`
			LatestQuote *struct {
				AskPrice float64 `json:"ap"`
				BidPrice float64 `json:"bp"`
			} `json:"latestQuote"`
		} `json:"snapshots"`
	} `json:"data"`
}

// summarizeOptionChain turns the raw chain response into a short,
// sorted, LLM-readable list — and separately returns every OCC symbol
// offered mapped to its quote, so the caller can verify the LLM's
// eventual choice was actually one of these (not hallucinated) and look
// up its premium for policy evaluation. Falls back to an empty
// summary/candidate map on any parse failure, same "never show raw
// JSON, never trust an unparseable response" principle as summarizeNews.
func summarizeOptionChain(raw string, underlying string) (string, map[string]OptionCandidate, error) {
	var parsed optionChainToolResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", nil, fmt.Errorf("parse option chain response: %w", err)
	}
	if len(parsed.Data.Snapshots) == 0 {
		return fmt.Sprintf("No tradeable option contracts found for %s in the target window.", underlying), nil, nil
	}

	symbols := make([]string, 0, len(parsed.Data.Snapshots))
	for sym := range parsed.Data.Snapshots {
		symbols = append(symbols, sym)
	}
	sort.Strings(symbols) // deterministic order; also happens to group by expiry/type/strike

	candidates := make(map[string]OptionCandidate, len(symbols))
	var lines []string
	greeksSeen := false
	for _, sym := range symbols {
		snap := parsed.Data.Snapshots[sym]
		parsedOpt, err := alpacaclient.ParseOCCSymbol(sym)
		if err != nil {
			continue // skip anything that doesn't parse rather than show a raw/broken line
		}

		bid, ask := 0.0, 0.0
		if snap.LatestQuote != nil {
			bid, ask = snap.LatestQuote.BidPrice, snap.LatestQuote.AskPrice
		}

		// VERIFIED via a real raw-response dump: this feed frequently
		// omits the "greeks" object from a snapshot entirely — not a
		// zero value, genuinely absent. Showing "delta 0.00" in that
		// case would read as a real calculated zero-delta contract
		// (directionally worthless) to both a human and the LLM, which
		// is actively misleading — the delta isn't zero, it's unknown.
		deltaStr := "n/a"
		if snap.Greeks != nil {
			deltaStr = fmt.Sprintf("%.2f", snap.Greeks.Delta)
			greeksSeen = true
		}

		candidates[sym] = OptionCandidate{AskPrice: ask, BidPrice: bid}
		lines = append(lines, fmt.Sprintf(
			"- %s: %s | bid $%.2f ask $%.2f | delta %s",
			sym, parsedOpt.Describe(), bid, ask, deltaStr,
		))
	}

	summary := strings.Join(lines, "\n")
	if !greeksSeen {
		summary = "Note: this data feed does not provide greeks for these contracts (delta shows as n/a for all of them) — judge moneyness from strike vs. current price instead.\n" + summary
	}

	return summary, candidates, nil
}

// callTool is the single choke point every tool invocation passes
// through — this is where the denylist is enforced, regardless of what
// the caller asks for or what ALPACA_TOOLSETS exposes.
func (c *Client) callTool(ctx context.Context, name string, args map[string]any) (string, error) {
	lower := strings.ToLower(name)
	for _, denied := range deniedToolSubstrings {
		if strings.Contains(lower, denied) {
			return "", fmt.Errorf("mcpclient: refusing to call tool %q — matches denylist %q (see package doc comment)", name, denied)
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	if args != nil {
		req.Params.Arguments = args
	}

	result, err := c.mcp.CallTool(ctx, req)
	if err != nil {
		return "", fmt.Errorf("call tool %s: %w", name, err)
	}
	if result.IsError {
		return "", fmt.Errorf("tool %s returned an error result", name)
	}

	var out strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			out.WriteString(text.Text)
			out.WriteString("\n")
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func (c *Client) Close() error {
	return c.mcp.Close()
}
