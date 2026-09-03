package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds every environment-driven setting the app needs.
// Loaded once at startup in cmd/server/main.go.
type Config struct {
	// Alpaca Go SDK (execution + quotes)
	AlpacaKeyID     string
	AlpacaSecretKey string
	AlpacaBaseURL   string // trading API — paper-api.alpaca.markets
	DataAPIBaseURL  string // market data API — data.alpaca.markets (separate client, see alpacaclient package)

	// Alpaca MCP server (agent context, read-only)
	MCPAlpacaAPIKey    string
	MCPAlpacaSecretKey string
	MCPPaperTrade      bool
	MCPToolsets        string

	// LLM
	FireworksAPIKey  string
	FireworksModel   string
	FireworksBaseURL string

	// PocketBase
	PocketbaseURL      string
	PocketbaseEmail    string
	PocketbasePassword string

	// News
	NewsAPIKey string

	// Agent behavior
	Watchlist             []string
	AgentIntervalMinutes  int
	ExecutorPollSeconds   int
	MaxPositionSizeUSD    float64
	DailyLossLimitUSD     float64
	MacroEventBufferHours int
}

// Load reads .env (if present) then environment variables, applying
// sane defaults where it's safe to do so. Fails loudly on anything
// required for placing real (paper) orders.
func Load() (*Config, error) {
	// Ignore error: .env is optional, real deployments use real env vars.
	_ = godotenv.Load()

	cfg := &Config{
		AlpacaKeyID:     mustGet("APCA_API_KEY_ID"),
		AlpacaSecretKey: mustGet("APCA_API_SECRET_KEY"),
		AlpacaBaseURL:   getOr("APCA_API_BASE_URL", "https://paper-api.alpaca.markets"),
		DataAPIBaseURL:  getOr("DATA_API_BASE_URL", "https://data.alpaca.markets"),

		MCPAlpacaAPIKey:    mustGet("ALPACA_API_KEY"),
		MCPAlpacaSecretKey: mustGet("ALPACA_SECRET_KEY"),
		MCPPaperTrade:      getOr("ALPACA_PAPER_TRADE", "True") == "True",
		// Verified default — excludes "trading" on purpose, see .env.example.
		MCPToolsets: getOr("ALPACA_TOOLSETS", "account,stock-data,options-data,news,assets"),

		FireworksAPIKey: mustGet("FIREWORKS_API_KEY"),
		// No fallback here on purpose (unlike the other *_BASE_URL configs
		// above) — Fireworks' serverless catalog rotates every few days
		// (confirmed: new models added within days of each other, older
		// ones retired). A hardcoded default model ID WILL go stale and
		// start 404ing, which is exactly what happened during development
		// with an earlier version of this file. Forcing this to be set
		// explicitly means you're always looking at your actual current
		// dashboard (app.fireworks.ai/dashboard/serverless) instead of
		// trusting a string I wrote once and never revisited.
		FireworksModel:   mustGet("FIREWORKS_MODEL"),
		FireworksBaseURL: getOr("FIREWORKS_BASE_URL", "https://api.fireworks.ai/inference/v1"),

		PocketbaseURL:      getOr("POCKETBASE_URL", "http://localhost:8090"),
		PocketbaseEmail:    mustGet("POCKETBASE_ADMIN_EMAIL"),
		PocketbasePassword: mustGet("POCKETBASE_ADMIN_PASSWORD"),

		NewsAPIKey: os.Getenv("NEWS_API_KEY"),

		Watchlist:             splitCSV(getOr("WATCHLIST", "AAPL,MSFT,NVDA,TSLA,SPY")),
		AgentIntervalMinutes:  mustInt(getOr("AGENT_INTERVAL_MINUTES", "15")),
		ExecutorPollSeconds:   mustInt(getOr("EXECUTOR_POLL_SECONDS", "10")),
		MaxPositionSizeUSD:    mustFloat(getOr("MAX_POSITION_SIZE_USD", "1000")),
		DailyLossLimitUSD:     mustFloat(getOr("DAILY_LOSS_LIMIT_USD", "500")),
		MacroEventBufferHours: mustInt(getOr("MACRO_EVENT_BUFFER_HOURS", "2")),
	}

	return cfg, nil
}

func mustGet(key string) string {
	v := os.Getenv(key)
	if v == "" {
		// Deliberately loud: a missing credential should fail startup,
		// not silently degrade into a broken agent.
		panic("missing required env var: " + key)
	}
	return v
}

func getOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func mustInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic("invalid integer env value: " + s)
	}
	return n
}

func mustFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic("invalid float env value: " + s)
	}
	return f
}
