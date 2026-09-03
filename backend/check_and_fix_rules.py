import json
import urllib.request

# --- Edit these to match your setup ---
EMAIL = "tobiccini@gmail.com"
PASSWORD = "1234567890"
POCKETBASE_URL = "http://127.0.0.1:8090"

# --- Auth ---
auth_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/_superusers/auth-with-password",
    data=json.dumps({"identity": EMAIL, "password": PASSWORD}).encode(),
    headers={"Content-Type": "application/json"},
)
token = json.load(urllib.request.urlopen(auth_req))["token"]
print("Authenticated.\n")

# --- Check current rules on proposed_trades ---
get_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/proposed_trades",
    headers={"Authorization": token},
)
current = json.load(urllib.request.urlopen(get_req))
print("Current rules on proposed_trades:")
print(f"  listRule:   {current.get('listRule')!r}")
print(f"  viewRule:   {current.get('viewRule')!r}")
print(f"  updateRule: {current.get('updateRule')!r}")
print(f"  createRule: {current.get('createRule')!r}")
print()

needs_fix = current.get("listRule") != "" or current.get("viewRule") != "" or current.get("updateRule") != ""

if not needs_fix:
    print("Rules already look correct (list/view/update all public).")
    print("The 400 might be something else — paste this output back for a closer look.")
else:
    print("Fixing rules now (setting list/view/update to public)...")
    collection_id = current["id"]
    patch_req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections/{collection_id}",
        data=json.dumps({"listRule": "", "viewRule": "", "updateRule": ""}).encode(),
        headers={"Content-Type": "application/json", "Authorization": token},
        method="PATCH",
    )
    urllib.request.urlopen(patch_req)
    print("Done. Rules updated — restart the Flutter app (hot restart, not just")
    print("hot reload) and the 400 should be gone.")