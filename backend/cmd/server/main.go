package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourusername/tradeguard/config"
	"github.com/yourusername/tradeguard/internal/agent"
	"github.com/yourusername/tradeguard/internal/alpacaclient"
	"github.com/yourusername/tradeguard/internal/execution"
	"github.com/yourusername/tradeguard/internal/llm"
	"github.com/yourusername/tradeguard/internal/mcpclient"
	"github.com/yourusername/tradeguard/internal/models"
	"github.com/yourusername/tradeguard/internal/news"
	"github.com/yourusername/tradeguard/internal/policy"
	"github.com/yourusername/tradeguard/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Execution-capable Alpaca client (Go SDK) ---
	// Two base URLs on purpose: trading and market data are separate
	// clients under the hood (see internal/alpacaclient's doc comment).
	alpacaCl := alpacaclient.New(cfg.AlpacaKeyID, cfg.AlpacaSecretKey, cfg.AlpacaBaseURL, cfg.DataAPIBaseURL)

	// --- Read-only MCP client for agent context ---
	mcpCl, err := mcpclient.New(ctx, mcpclient.Config{
		APIKey:     cfg.MCPAlpacaAPIKey,
		SecretKey:  cfg.MCPAlpacaSecretKey,
		PaperTrade: cfg.MCPPaperTrade,
		Toolsets:   cfg.MCPToolsets,
	})
	if err != nil {
		log.Fatalf("mcp client: %v", err)
	}
	defer mcpCl.Close()

	// --- PocketBase store ---
	st := store.New(cfg.PocketbaseURL)
	if err := st.Authenticate(ctx, cfg.PocketbaseEmail, cfg.PocketbasePassword); err != nil {
		log.Fatalf("pocketbase auth: %v", err)
	}
	if err := st.EnsureSystemConfig(ctx); err != nil {
		// Non-fatal: IsPaused() already treats "no record found" as
		// not-paused, so this failing just means you can't toggle the
		// kill switch from Flutter until a record exists — not that
		// anything is broken or unsafe.
		log.Printf("warning: could not seed system_config: %v", err)
	}

	// --- LLM reasoning client ---
	llmCl := llm.New(cfg.FireworksAPIKey, cfg.FireworksModel, cfg.FireworksBaseURL)

	// --- Policy engine ---
	policyEngine := policy.New(models.PolicyConfig{
		MaxPositionSizeUSD:    cfg.MaxPositionSizeUSD,
		DailyLossLimitUSD:     cfg.DailyLossLimitUSD,
		MacroEventBufferHours: cfg.MacroEventBufferHours,
	})

	ag := &agent.Agent{
		Watchlist:   cfg.Watchlist,
		Alpaca:      alpacaCl,
		MCP:         mcpCl,
		LLM:         llmCl,
		Policy:      policyEngine,
		Store:       st,
		MacroEvents: news.HardcodedCalendar(),
	}

	ex := &execution.Executor{
		Alpaca: alpacaCl,
		Store:  st,
	}

	agentTicker := time.NewTicker(time.Duration(cfg.AgentIntervalMinutes) * time.Minute)
	execTicker := time.NewTicker(time.Duration(cfg.ExecutorPollSeconds) * time.Second)
	defer agentTicker.Stop()
	defer execTicker.Stop()

	log.Printf("tradeguard: watching %v, agent every %dm, executor every %ds",
		cfg.Watchlist, cfg.AgentIntervalMinutes, cfg.ExecutorPollSeconds)

	// Run once immediately on startup instead of waiting for the first tick.
	ag.RunOnce(ctx)
	ex.RunOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("tradeguard: shutting down")
			return
		case <-agentTicker.C:
			ag.RunOnce(ctx)
		case <-execTicker.C:
			ex.RunOnce(ctx)
		}
	}
}
