import json
import urllib.request

# --- Edit these to match your setup ---
EMAIL = "tobiccini@gmail.com"
PASSWORD = "1234567890"
POCKETBASE_URL = "http://127.0.0.1:8090"

auth_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/_superusers/auth-with-password",
    data=json.dumps({"identity": EMAIL, "password": PASSWORD}).encode(),
    headers={"Content-Type": "application/json"},
)
token = json.load(urllib.request.urlopen(auth_req))["token"]
print("Authenticated.\n")

get_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/proposed_trades",
    headers={"Authorization": token},
)
trades = json.load(urllib.request.urlopen(get_req))

field_names = [f["name"] for f in trades["fields"]]
new_fields = list(trades["fields"])
added = []
for name in ("underlying", "contract_description"):
    if name not in field_names:
        new_fields.append({"name": name, "type": "text"})
        added.append(name)

if added:
    patch_req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections/{trades['id']}",
        data=json.dumps({"fields": new_fields}).encode(),
        headers={"Content-Type": "application/json", "Authorization": token},
        method="PATCH",
    )
    urllib.request.urlopen(patch_req)
    print(f"proposed_trades: added {added}")
else:
    print("proposed_trades: underlying/contract_description already present, nothing to do")

print("\nDone. Restart your Go server to pick this up.")
print("Note: old trades in your queue won't have these fields populated —")
print("that's fine, only new agent-generated proposals will use them.")
