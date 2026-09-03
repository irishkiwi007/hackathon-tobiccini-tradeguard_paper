import json
import urllib.request

# --- Edit these to match your setup ---
EMAIL = "tobiccini@gmail.com"
PASSWORD = "1234567890"
POCKETBASE_URL = "http://127.0.0.1:8090"

# --- Edit these to change what the test trade looks like ---
SYMBOL = "AAPL"      # keep it on the watchlist so it looks natural in context
SIDE = "buy"
QTY = 1              # small and cheap — ~$300 notional at recent AAPL prices

# --- Auth ---
auth_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/_superusers/auth-with-password",
    data=json.dumps({"identity": EMAIL, "password": PASSWORD}).encode(),
    headers={"Content-Type": "application/json"},
)
token = json.load(urllib.request.urlopen(auth_req))["token"]
print("Authenticated.")

# --- Insert the test trade ---
trade = {
    "symbol": SYMBOL,
    "side": SIDE,
    "qty": QTY,
    "reasoning": (
        "TEST TRADE — inserted manually via script, not agent-generated. "
        "Used to verify the full approve -> execute pipeline end to end."
    ),
    "news_context": "",
    "risk_flags": ["TEST TRADE — manually inserted, not from the agent loop"],
    "status": "pending",
}

create_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/proposed_trades/records",
    data=json.dumps(trade).encode(),
    headers={"Content-Type": "application/json", "Authorization": token},
    method="POST",
)
resp = json.load(urllib.request.urlopen(create_req))
print(f"Created pending trade: {SIDE} {QTY} {SYMBOL}")
print(f"Record ID: {resp['id']}")
print("\nIt should now show up in the Flutter app's Approval Queue.")
print("Approve it there, then watch your `go run ./cmd/server` terminal —")
print("the executor polls every 10s and should pick it up shortly after.")
