# TradeGuard (working name)

Human-in-the-loop trading agent for the Alpaca AI Trading Agents Hackathon.
An agent proposes trades via Alpaca's MCP server (read-only context
only); nothing executes until a human approves it in the Flutter app
(`tradeguard_app/`, separate package). Full design rationale lives in
`alpaca-hackathon-architecture.md` from the planning conversation — this
repo implements that design.

## Status

Scaffold, but the core unknown is now resolved AND verified working
end-to-end against real paper credentials — real account data and a
real live quote came back successfully. Along the way, a genuine
deadlock bug was found and fixed (see below) — not a stub anymore,
this actually runs.

## A real deadlock, found and fixed during development

The first working test hung indefinitely on the very first tool call
(`get_account_info`), even though the MCP handshake succeeded instantly.
After ruling out network, TLS, and the Python server itself (all
verified fine independently), the actual cause turned out to be a
classic subprocess pipe deadlock: `internal/mcpclient` was never
reading the subprocess's **stderr** pipe. FastMCP prints a sizeable
startup banner and log lines to stderr — once that pipe's OS buffer
filled up with nobody draining it, the Python process **blocked** on
its own next stderr write, which stalled it before it could finish
writing the actual response to stdout. From the Go side this looked
exactly like the server hanging forever.

Fixed in `internal/mcpclient.New()` — it now drains stderr
unconditionally in a background goroutine for the life of the client,
logging it with an `[alpaca-mcp-server]` prefix. This isn't optional
diagnostic logging, it's load-bearing: removing it reintroduces the
deadlock. If you ever rewrite this constructor, keep the drain.

## A PocketBase 5000-character text limit, too

The first real agent-generated trade proposal (not the manually
inserted test one) failed to write to PocketBase — a plain `text`
field defaults to a 5000-character max, which `news_context` can
exceed once it's carrying several real articles back from `get_news`.
Two fixes, both real: the collections now set `max: 50000` on
`reasoning`/`news_context`/`notes` explicitly (note `max: 0` does NOT
mean unlimited here — it just falls back to the 5000 default, tested
and confirmed), AND `internal/store`'s error handling now includes
PocketBase's actual response body on a failed write, not just the
HTTP status code. That second fix is the more durable one — it's what
made this actually diagnosable instead of guesswork, and will do the
same for whatever's discovered next.

## A PocketBase time-parsing finding too

The executor's `ApprovedTrades()` call failed on the very first real
approved trade — Go's `time.Time` JSON unmarshaling strictly expects
RFC3339 (`2026-08-18T07:41:17Z`, literal `T` separator), but
PocketBase's actual date output uses a space instead
(`2026-08-18 07:41:17.386Z`). One bad field fails the whole decode, so
this quietly broke every executor poll rather than just one field.
Fixed with a small `models.PBTime` wrapper type with its own
`UnmarshalJSON` matching PocketBase's real layout (falls back to
RFC3339 defensively). If you add more `time.Time`-typed fields that get
read back from PocketBase later, use `PBTime` for those too — plain
`time.Time` will silently break the same way.

## A Fireworks/Kimi finding worth knowing about too

During the first real run, the originally-suggested Fireworks model
(`llama-v3p1-70b-instruct`) turned out to no longer be available —
Fireworks' serverless catalog rotates over time, so don't treat any
specific model name in this codebase's history as durable. The
replacement that got picked (`kimi-k3`) is a reasoning model, which
surfaced a second, more interesting issue: Fireworks bills reasoning
tokens against the same `max_tokens` budget as the actual answer, so an
unbounded reasoning trace can silently crowd out or truncate the JSON
this code needs to parse. Fixed in `internal/llm/fireworks.go` by
sending `reasoning_effort: "none"` — verified against Fireworks' own
API reference, not a guess. Worth knowing if you ever swap models again
and parsing starts failing intermittently: check whether the new model
reasons by default first.

## Kill switch and bull/bear case (added after the initial build)

Two small additions on top of the proven core loop:

- **Kill switch** — a `system_config` collection with one `paused`
  record. Both `agent.RunOnce()` and `executor.RunOnce()` check it at
  the top of every pass. Pausing stops new proposals AND stops
  execution of already-approved trades — but never cancels or drops
  anything already approved, it just waits until resumed. Toggled from
  the Flutter app's Queue screen (AppBar icon), same pattern as
  approve/deny: pure PocketBase write, no Go round trip needed for the
  toggle itself to take effect.
- **Bull case / bear case** — the LLM's JSON response now includes two
  extra short fields alongside `reasoning`, shown side by side in the
  Flutter approval card. Cheap to add (same request, two more fields),
  and it visibly shows a human that both sides were weighed rather than
  just reading a conclusion.

Run `patch_kill_switch_and_bullbear.py` against your existing PocketBase
instance to add both without losing any existing data — verified
non-destructive against a live-equivalent test instance before being
shared.

## A safety finding worth understanding before you touch this code

While wiring the real MCP client, I cloned Alpaca's actual MCP server
source to verify tool names. Found something important: the server's
`"trading"` toolset (which has useful read tools like `getAllOrders`
and `getAllOpenPositions`) **also auto-registers order-placement tools**
(`place_stock_order`, `place_crypto_order`, `place_option_order`) the
moment it's enabled. There's no config option to get trading-toolset
reads without also getting order placement.

That would have quietly undermined the whole pitch — "the LLM literally
cannot place an order" stops being true the moment you enable "trading"
for a seemingly unrelated reason. Two things fix it:

1. `ALPACA_TOOLSETS` is set to `account,stock-data,news,assets` —
   `"trading"` is deliberately excluded. Position/order data for the
   agent instead comes from `internal/alpacaclient.Positions()`, which
   hits Alpaca's Trading API directly via the Go SDK — a path with no
   order-placement capability wired to it at all.
2. `internal/mcpclient`'s `callTool` also hard-refuses any tool name
   containing "order" or "delete", regardless of what the server
   advertises. Defense in depth — if someone edits `ALPACA_TOOLSETS`
   later without reading this, it fails safe instead of silently
   handing the LLM an execution tool.

If you're demoing this to judges, this is worth a slide. "We found and
closed a real gap between the SDK's config surface and its actual
behavior" is a much stronger technical story than "we called an API."

## Quick start

1. **Resolve dependencies.** This was scaffolded in a sandboxed
   environment with no access to the Go module proxy, so `go.sum` isn't
   included. Method names, struct fields, and package paths WERE
   verified by cloning the real source of every dependency directly
   from GitHub — only the exact version pins are unverified. On your
   own machine:
   ```
   go mod tidy
   ```

2. **Install the Alpaca MCP server** (confirmed published on PyPI):
   ```
   pip install alpaca-mcp-server
   # or: uvx alpaca-mcp-server   (no separate install step)
   ```
   The Go backend spawns this as a subprocess — make sure the
   `alpaca-mcp-server` command is on your `PATH`.

3. **Get Alpaca paper credentials** and copy them into `.env`:
   ```
   cp .env.example .env
   # fill in APCA_API_KEY_ID / APCA_API_SECRET_KEY / ALPACA_API_KEY / ALPACA_SECRET_KEY
   ```

4. **Start PocketBase:**
   ```
   docker compose up -d pocketbase
   ```
   Open `http://localhost:8090/_/` and create an admin/superuser
   account matching `POCKETBASE_ADMIN_EMAIL` / `POCKETBASE_ADMIN_PASSWORD`
   in your `.env`.

5. **Create the three PocketBase collections** (admin UI, or via the
   API):

   **`proposed_trades`**
   | field | type |
   |---|---|
   | symbol | text |
   | side | text |
   | qty | number |
   | reasoning | text |
   | news_context | text |
   | risk_flags | json |
   | status | text (pending / approved / denied / executed / failed) |

   **`audit_log`**
   | field | type |
   |---|---|
   | trade_id | text |
   | decision | text |
   | decided_by | text |
   | notes | text |
   | decided_at | date |

   **`account_snapshots`** (populates the Flutter portfolio screen's
   equity card — written automatically once per agent pass)
   | field | type |
   |---|---|
   | equity | number |
   | buying_power | number |
   | day_pnl | number |

   Also set List/View/Update rules on `proposed_trades`, List/Create on
   `audit_log`, and List on `account_snapshots` to public (empty
   string) — the Flutter app has no auth and needs these open for the
   demo. See `tradeguard_app/README.md`.

6. **Run it:**
   ```
   go run ./cmd/server
   ```
   Watch the logs for the MCP initialize handshake succeeding — that's
   confirmation the subprocess + MCP protocol wiring actually works
   end-to-end against your real keys.

## Package map

- `internal/agent` — proposes trades, never executes them. Pulls
  account context from MCP, positions from the Go SDK (see safety
  finding above), news from `internal/news`.
- `internal/mcpclient` — real MCP client, read-only toolset + denylist.
  Also the single source of headlines now (`News` method calling the
  real `get_news` tool) — no separate news provider/API needed.
- `internal/alpacaclient` — the only package allowed to place a real
  order. Two underlying clients (trading + market data) — verified from
  SDK source, not assumed.
- `internal/execution` — polls for human-approved trades, calls
  alpacaclient.
- `internal/policy` — risk flags (size cap, daily loss, macro event
  buffer).
- `internal/news` — macro calendar only now (headlines moved to
  `mcpclient`). Has one verified real event: the Aug 2026 jobs report,
  confirmed against bls.gov, releasing Sept 4 — the last day of build
  week.
- `internal/store` — PocketBase REST client. Also writes
  `account_snapshots` once per agent pass now, for the Flutter
  portfolio screen's equity card.
- `internal/llm` — Fireworks AI chat completion wrapper.

## Known gaps to fill before demo day

- Exact dependency versions in `go.mod` — resolve with `go mod tidy`
  on a machine with normal internet access
