# TradeGuard — Technical Write-Up

## AI logic

TradeGuard's agent runs every 15 minutes across a 5-symbol watchlist,
making one LLM call per symbol (Fireworks AI, Kimi K3, `reasoning_effort:
none`). Each call reads real, current context through a read-only MCP
connection to Alpaca: account state, existing positions, recent news, and
a pre-filtered option chain. The chain is screened in Go *before* the
model ever sees it — 25–45 day expiry, strikes within ±12% of spot — so
the LLM is choosing from real, already-viable candidates, never
constructing a contract from scratch.

The model returns structured JSON (`propose`, `option_symbol`, `qty`,
`reasoning`, `bull_case`, `bear_case`). It's given the account's actual
position-size cap and a strict, linear sizing procedure — best contract,
does qty=1 fit, if not the closest-fitting same-direction alternative,
if nothing fits then no trade — rather than an open-ended constraint.
That specificity matters in practice: an earlier, looser version of this
instruction caused the model to narrate rejected candidates inside the
JSON output when a proposal didn't cleanly fit, overrunning the response
token budget and breaking parsing. The agent proposes; it never decides.
A human approves or denies every single proposal before anything can
execute.

## Risk gates

- **Defined risk only.** Long calls and puts to open a position —
  no selling, no multi-leg, no closing. Max loss is always the premium
  paid, known upfront.
- **Position-size cap, enforced twice, independently.** The app disables
  Approve outright for any proposal over cap. The executor refuses to
  place the order even if a trade reaches "approved" status some other
  way (a stale client, a direct database write) — it doesn't trust the
  UI as the only gate.
- **Kill switch**, checked by both the agent and the executor before
  anything happens. When paused, the agent skips its LLM call entirely
  for that symbol — not just the resulting trade — and fails *open* on
  a read error, so a failure mode never silently blocks a human's own
  approval.
- **No order-placement capability in the LLM's toolset at all**, plus a
  denylist blocking any tool named "order" or "delete" as a second,
  independent check.
- **Every proposed contract is validated** against the real, pre-filtered
  candidate list before it's stored — a hallucinated symbol is rejected
  before it can ever reach an order.
- **Every outcome is explained, not just the successes.** Approved,
  denied, executed, refused for exceeding cap, or rejected by the
  broker — each writes a reason to a permanent audit trail. A rejection
  that's purely timing-based (an order placed outside market hours) is
  treated as retryable and requeued automatically once the market
  reopens, rather than dead-ending as an unexplained failure.

## Alpaca infrastructure

Two separate paths into Alpaca, deliberately never wired to the same
capability: a read-only MCP client supplies market context (account
info, quotes, news, option chains) to the agent, and a completely
separate Go path — using the Alpaca Trading API directly — handles
execution. The LLM only ever has access to the first. Orders are placed
solely by the executor, solely after human approval, never by the
agent process itself.

PocketBase (`proposed_trades`, `audit_log`, `account_snapshots`,
`system_config`) is the single source of truth; the Flutter app
subscribes to it live. The whole backend — the Go binary and the
Python-based Alpaca MCP server it runs as a subprocess — ships as one
Docker image, deployed on Railway with PocketBase on a persistent
volume so trade and audit history survives restarts and redeploys
rather than resetting to empty.

**Alpaca paper trading account ID:** `[ fill in once the fresh account
for judging is created ]`
