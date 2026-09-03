import json
import urllib.request

# --- Edit these to match your setup ---
EMAIL = "tobiccini@gmail.com"
PASSWORD = "1234567890"
POCKETBASE_URL = "http://127.0.0.1:8090"

COLLECTIONS = ["proposed_trades", "audit_log", "account_snapshots"]

# --- Auth ---
auth_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/_superusers/auth-with-password",
    data=json.dumps({"identity": EMAIL, "password": PASSWORD}).encode(),
    headers={"Content-Type": "application/json"},
)
try:
    token = json.load(urllib.request.urlopen(auth_req))["token"]
except Exception as e:
    print(f"AUTH FAILED: {e}")
    print("Check EMAIL/PASSWORD match your current superuser, and that")
    print(f"PocketBase is actually running and reachable at {POCKETBASE_URL}")
    raise SystemExit(1)

print("Authenticated.\n")

for name in COLLECTIONS:
    get_req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections/{name}",
        headers={"Authorization": token},
    )
    try:
        current = json.load(urllib.request.urlopen(get_req))
    except urllib.error.HTTPError as e:
        print(f"{name}: COULD NOT FETCH ({e.code}) — does this collection exist at all?")
        continue

    field_names = [f["name"] for f in current["fields"]]
    has_created = "created" in field_names
    has_updated = "updated" in field_names

    print(f"{name}: fields = {field_names}")

    if has_created and has_updated:
        print(f"  -> already has created/updated, nothing to do\n")
        continue

    print(f"  -> MISSING created={not has_created} updated={not has_updated}, patching now...")

    new_fields = list(current["fields"])  # keep existing fields (with their ids) untouched
    if not has_created:
        new_fields.append({"name": "created", "type": "autodate", "onCreate": True, "onUpdate": False})
    if not has_updated:
        new_fields.append({"name": "updated", "type": "autodate", "onCreate": True, "onUpdate": True})

    patch_req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections/{current['id']}",
        data=json.dumps({"fields": new_fields}).encode(),
        headers={"Content-Type": "application/json", "Authorization": token},
        method="PATCH",
    )
    try:
        urllib.request.urlopen(patch_req)
        print(f"  -> patched successfully\n")
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        print(f"  -> PATCH FAILED ({e.code}): {body}\n")

print("Done. If anything got patched, restart the Flutter app (hot restart,")
print("not hot reload) and try again.")
