# TradeGuard — Approval App (Flutter)

The mobile half of the human-in-the-loop trading agent. Talks to the
same PocketBase instance as the Go backend — never talks to Alpaca
directly, and holds no Alpaca credentials. See the root project's
`alpaca-hackathon-architecture.md` for the full design rationale.

## Status

Scaffold, not finished, but the realtime wiring is verified correct —
`pb.collection('proposed_trades').subscribe('*', ...)` matches the
official PocketBase Dart SDK docs verbatim. One confirmed limitation:
realtime subscriptions don't work on Flutter Web, only on native
platforms — not an issue here since the demo targets a physical device
anyway.

## Quick start

1. **Generate platform folders.** This scaffold only includes `lib/` and
   config files — no `android/`, `ios/`, etc. From this directory:
   ```
   flutter create .
   ```
   This adds the missing platform folders without touching `lib/` or
   `pubspec.yaml`.

2. **Resolve dependencies:**
   ```
   flutter pub get
   ```

3. **Open up PocketBase API rules** so this unauthenticated app can read
   and write. In the PocketBase admin UI (`http://localhost:8090/_/`):
   - `proposed_trades` collection → set **List**, **View**, and
     **Update** rules to empty string (public)
   - `audit_log` collection → set **List** and **Create** rules to
     empty string (public)
   - `account_snapshots` collection → set **List** to empty string
     (populated automatically by the Go backend now — see root README)

   This is fine for a single-user hackathon demo. Don't ship it this
   way — add PocketBase user auth before this touches anyone else's
   money or data.

4. **Point the app at your PocketBase instance.** Run on a physical
   device (recommended for the demo — push-style realtime updates
   landing on your phone is the whole point):
   ```
   flutter run --dart-define=POCKETBASE_URL=http://<your-lan-ip>:8090
   ```
   Find your LAN IP with `ipconfig getifaddr en0` (Mac) or `ip addr`
   (Linux). Your phone and dev machine need to be on the same network.

## Screens

- **Queue** — realtime list of pending proposals. Approve/deny writes
  directly to `proposed_trades` and `audit_log`; the Go executor picks
  up approvals on its next poll.
- **Portfolio** — equity/buying-power/day P&L card, written by the Go
  agent loop once per pass, plus an activity feed of executed/failed
  trades. Both work out of the box once you've created all three
  PocketBase collections (see root README).
- **Audit** — every decision ever made, newest first. This is the
  explainability screen — lean on it in the demo.

## Known gaps before demo day

- No push notifications when the app is backgrounded/closed — realtime
  updates only fire while the app is open. Fine for a live demo, a real
  gap for an actual product (FCM would be the next step)
- No auth — see the PocketBase rules note above
