// Command mcptest isolates ONE thing: does the MCP handshake against
// Alpaca's server actually work with your real keys? It doesn't touch
// PocketBase, Fireworks AI, or anything else in the stack — just spawns
// the MCP server subprocess, initializes, and calls two real tools.
//
// Run it with:
//
//	go run ./cmd/mcptest
//
// Needs only ALPACA_API_KEY / ALPACA_SECRET_KEY in your .env — nothing
// else from the full config is required for this test.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/yourusername/tradeguard/internal/mcpclient"
)

func main() {
	_ = godotenv.Load() // optional — real env vars work too

	apiKey := os.Getenv("ALPACA_API_KEY")
	secretKey := os.Getenv("ALPACA_SECRET_KEY")
	if apiKey == "" || secretKey == "" {
		log.Fatal("mcptest: set ALPACA_API_KEY and ALPACA_SECRET_KEY (in .env or your shell) before running this")
	}

	toolsets := os.Getenv("ALPACA_TOOLSETS")
	if toolsets == "" {
		toolsets = "account,stock-data,options-data,news,assets" // verified safe default — see internal/mcpclient
	}

	fmt.Println("→ starting alpaca-mcp-server subprocess and running the MCP initialize handshake...")
	fmt.Println("  (first run can be slow — Python is importing alpaca-py/fastmcp cold)")

	// Each operation gets its OWN timeout instead of sharing one deadline
	// across the whole sequence. A shared deadline is the classic mistake
	// here: subprocess startup + package imports can eat 15-20s on their
	// own (worse on Windows), leaving too little budget for the actual
	// tool calls that follow — which is exactly what produced the
	// "context deadline exceeded" error on the first pass of this file.
	handshakeCtx, handshakeCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer handshakeCancel()

	c, err := mcpclient.New(handshakeCtx, mcpclient.Config{
		APIKey:     apiKey,
		SecretKey:  secretKey,
		PaperTrade: true,
		Toolsets:   toolsets,
	})
	if err != nil {
		log.Fatalf("✗ handshake failed: %v\n\n"+
			"Common causes:\n"+
			"  - `alpaca-mcp-server` isn't on your PATH — try `pip install alpaca-mcp-server` first\n"+
			"  - wrong API key/secret\n"+
			"  - Python version too old (server requires 3.10+)\n", err)
	}
	defer c.Close()

	fmt.Println("✓ handshake succeeded — subprocess is alive and speaking MCP")
	fmt.Println("  (server stderr, if any, will print above with an [alpaca-mcp-server] prefix — that's now automatic)")

	fmt.Println("\n→ calling get_account_info...")
	acctCtx, acctCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer acctCancel()
	acct, err := c.AccountSummary(acctCtx)
	if err != nil {
		log.Fatalf("✗ AccountSummary failed: %v\n\n"+
			"If this is ALSO a timeout, the subprocess likely can't reach "+
			"Alpaca's API over the network (firewall/VPN/proxy) — try "+
			"running `alpaca-mcp-server` directly in another terminal to "+
			"see its own error output.", err)
	}
	fmt.Println("✓ account summary:")
	fmt.Println(" ", acct)

	fmt.Println("\n→ calling get_stock_latest_quote for AAPL...")
	quoteCtx, quoteCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer quoteCancel()
	quote, err := c.Quote(quoteCtx, "AAPL")
	if err != nil {
		log.Fatalf("✗ Quote failed: %v", err)
	}
	fmt.Println("✓ quote:")
	fmt.Println(" ", quote)

	fmt.Println("\n→ calling get_news for AAPL...")
	newsCtx, newsCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer newsCancel()
	headlines, err := c.News(newsCtx, "AAPL")
	if err != nil {
		log.Fatalf("✗ News failed: %v", err)
	}
	fmt.Println("✓ news:")
	fmt.Println(" ", headlines)

	fmt.Println("\n→ calling get_option_chain for AAPL...")
	fmt.Println("  (using an approximate reference price — this is just a wiring smoke test, not a real trade)")
	chainCtx, chainCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer chainCancel()
	chainSummary, candidates, err := c.OptionChain(chainCtx, "AAPL", 150.0)
	if err != nil {
		log.Fatalf("✗ OptionChain failed: %v", err)
	}
	fmt.Printf("✓ option chain: %d contracts offered\n", len(candidates))
	fmt.Println(chainSummary)

	fmt.Println("\n→ fetching the RAW (unparsed) option chain response for AAPL...")
	fmt.Println("  (diagnostic only — this is what to paste back if deltas/prices look wrong above)")
	rawCtx, rawCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer rawCancel()
	rawChain, err := c.OptionChainRaw(rawCtx, "AAPL", 150.0)
	if err != nil {
		log.Fatalf("✗ OptionChainRaw failed: %v", err)
	}
	fmt.Println("✓ raw response:")
	fmt.Println(rawChain)

	fmt.Println("\nAll good — the MCP wiring works end-to-end against your real paper account.")
}
