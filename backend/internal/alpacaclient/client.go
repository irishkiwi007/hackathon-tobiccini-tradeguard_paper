// Package alpacaclient wraps the official Alpaca Go SDK
// (github.com/alpacahq/alpaca-trade-api-go/v3). It is the ONLY place in
// this codebase allowed to place a real (paper) order.
//
// The agent/LLM never touches this package directly — it only sees the
// MCP server's read-only tools (see internal/mcpclient). A ProposedTrade
// becomes a real order exclusively through Executor.Run, and only after
// a human has flipped its status to "approved". That separation is the
// whole safety story for this project — keep it that way.
//
// VERIFIED against github.com/alpacahq/alpaca-trade-api-go/v3 source
// (not guessed): the trading client (package alpaca) and the
// market-data client (package marketdata) are TWO SEPARATE clients
// hitting two different base URLs. GetLatestQuote lives on the
// marketdata client, not the trading client — easy mistake to make
// since they're both "the Alpaca client" conceptually.
package alpacaclient

import (
	"fmt"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/shopspring/decimal"
	"github.com/yourusername/tradeguard/internal/models"
)

type Client struct {
	trading *alpaca.Client
	data    *marketdata.Client
}

// New builds both underlying clients from the same paper credentials.
// dataBaseURL is normally https://data.alpaca.markets for both paper
// and live — market data isn't split by paper/live the way trading is.
func New(apiKey, apiSecret, tradingBaseURL, dataBaseURL string) *Client {
	trading := alpaca.NewClient(alpaca.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   tradingBaseURL,
	})
	data := marketdata.NewClient(marketdata.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   dataBaseURL,
	})
	return &Client{trading: trading, data: data}
}

// Account returns current paper account state — buying power, equity,
// P&L — used both by the agent (context) and the Flutter portfolio view.
// Alpaca's SDK returns these as decimal.Decimal, not float64, to avoid
// floating-point rounding on money — converted to float64 here at the
// boundary since our internal models keep things simple for a hackathon.
// If you need cent-level precision anywhere downstream, keep it as
// decimal.Decimal instead of converting this early.
func (c *Client) Account() (*alpaca.Account, error) {
	return c.trading.GetAccount()
}

// Positions returns current open paper positions directly from Alpaca's
// Trading API via the Go SDK — deliberately NOT sourced through the MCP
// server. Position/order awareness for the agent's context flows through
// this already-trusted read path instead of adding "trading" to the
// MCP toolset, because enabling that toolset on the MCP server also
// auto-registers its order-placement tools (place_stock_order etc.) —
// see internal/mcpclient's doc comment for the full explanation. This
// keeps the "LLM cannot place an order" guarantee intact while still
// giving the agent position context.
func (c *Client) Positions() ([]alpaca.Position, error) {
	return c.trading.GetPositions()
}

// LatestQuote fetches the current bid/ask for a symbol. Used by the
// agent to sanity-check price context before proposing a trade size.
func (c *Client) LatestQuote(symbol string) (float64, error) {
	q, err := c.data.GetLatestQuote(symbol, marketdata.GetLatestQuoteRequest{})
	if err != nil {
		return 0, fmt.Errorf("get latest quote for %s: %w", symbol, err)
	}
	// VERIFIED via a real mcptest raw-response dump, not assumed: this
	// account's feed routinely returns AskPrice=0 for stock quotes
	// (confirmed for AAPL specifically) while BidPrice is populated
	// normally. The original naive (ask+bid)/2 silently produced a
	// price roughly HALF the real one whenever that happened —
	// $150.47 instead of AAPL's real ~$300.93 — which then miscentered
	// an entire options strike-selection window in production and made
	// every contract shown look wildly mispriced relative to what the
	// LLM (correctly) understood the real price to be from other
	// context. Real, confirmed impact, not a hypothetical edge case.
	switch {
	case q.AskPrice == 0 && q.BidPrice > 0:
		return q.BidPrice, nil
	case q.BidPrice == 0 && q.AskPrice > 0:
		return q.AskPrice, nil
	default:
		return (q.AskPrice + q.BidPrice) / 2, nil
	}
}

// PlaceOrder submits a market order for an APPROVED trade. This must
// only ever be called by internal/execution.Executor, after the
// ProposedTrade's status is "approved" in the store.
//
// Works for both stock orders (Symbol is a ticker) and options orders
// (Symbol is an OCC option symbol) — same /v2/orders endpoint, same
// PlaceOrderRequest shape, verified against Alpaca's own trading-api
// spec. The one addition for options: PositionIntent tells Alpaca
// whether this opens or closes a position. This project only ever buys
// calls/puts to OPEN a new long position (never sells/writes options,
// never closes one) — see internal/agent's system prompt — so
// BuyToOpen is always correct here, not a per-trade decision.
func (c *Client) PlaceOrder(trade models.ProposedTrade) (orderID string, err error) {
	qty := decimal.NewFromFloat(trade.Qty)

	req := alpaca.PlaceOrderRequest{
		Symbol:      trade.Symbol,
		Qty:         &qty,
		Side:        alpaca.Side(trade.Side),
		Type:        alpaca.Market,
		TimeInForce: alpaca.Day, // the only valid value for options orders; also what stock orders already used
	}
	if IsOptionSymbol(trade.Symbol) {
		req.PositionIntent = alpaca.BuyToOpen
	}

	order, err := c.trading.PlaceOrder(req)
	if err != nil {
		return "", fmt.Errorf("place order for %s: %w", trade.Symbol, err)
	}
	return order.ID, nil
}

// DayPnLUSD returns the account's equity change since yesterday's
// close — positive for a gain, negative for a loss. This is the signed
// counterpart to DayLossUSD below; the portfolio UI wants to show gains
// in green, so it needs the sign that a risk check doesn't care about.
func DayPnLUSD(acct *alpaca.Account) float64 {
	equity, _ := acct.Equity.Float64()
	lastEquity, _ := acct.LastEquity.Float64()
	return equity - lastEquity
}

// DayLossUSD returns how much the account's equity has fallen since
// yesterday's close, floored at 0 (a flat or up day reports 0 loss, not
// a negative number). This is EQUITY-based day P&L, not strictly
// "realized" P&L in the traditional accounting sense — it also reflects
// unrealized mark-to-market moves on open positions, not just gains or
// losses locked in by closing a trade.
//
// That's a deliberate choice for a risk circuit breaker, not an
// oversight: it should react to money actually bleeding out of the
// account right now, whether or not that loss has been "realized" by
// closing a position. Reconstructing strictly realized P&L would mean
// matching buy/sell pairs and tracking cost basis per lot — real
// complexity that wouldn't make the circuit breaker meaningfully safer
// for what this project needs.
func DayLossUSD(acct *alpaca.Account) float64 {
	pnl := DayPnLUSD(acct)
	if pnl >= 0 {
		return 0
	}
	return -pnl
}

// AccountSummaryText renders a short plain-language account snapshot for
// the agent's LLM context — buying power and equity, formatted as a
// sentence rather than raw JSON, since that's what goes into the prompt.
func AccountSummaryText(acct *alpaca.Account) string {
	buyingPower, _ := acct.BuyingPower.Float64()
	equity, _ := acct.Equity.Float64()
	return fmt.Sprintf("Equity: $%.2f, Buying power: $%.2f", equity, buyingPower)
}

// PositionsSummaryText renders open positions as a short plain-language
// list for the agent's LLM context.
func PositionsSummaryText(positions []alpaca.Position) string {
	if len(positions) == 0 {
		return "No open positions."
	}
	out := "Open positions: "
	for i, p := range positions {
		qty, _ := p.Qty.Float64()
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%s x%.4f", p.Symbol, qty)
	}
	return out
}
