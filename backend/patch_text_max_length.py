import json
import urllib.request

# --- Edit these to match your setup ---
EMAIL = "tobiccini@gmail.com"
PASSWORD = "1234567890"
POCKETBASE_URL = "http://127.0.0.1:8090"

# collection -> {field name: new max length}
FIXES = {
    "proposed_trades": {"reasoning": 50000, "news_context": 50000},
    "audit_log": {"notes": 50000},
}

auth_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/_superusers/auth-with-password",
    data=json.dumps({"identity": EMAIL, "password": PASSWORD}).encode(),
    headers={"Content-Type": "application/json"},
)
token = json.load(urllib.request.urlopen(auth_req))["token"]
print("Authenticated.\n")

for collection_name, field_fixes in FIXES.items():
    get_req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections/{collection_name}",
        headers={"Authorization": token},
    )
    current = json.load(urllib.request.urlopen(get_req))

    changed = False
    new_fields = []
    for f in current["fields"]:
        if f["name"] in field_fixes and f.get("type") == "text":
            old_max = f.get("max")
            f = dict(f)
            f["max"] = field_fixes[f["name"]]
            print(f"{collection_name}.{f['name']}: max {old_max} -> {f['max']}")
            changed = True
        new_fields.append(f)

    if not changed:
        print(f"{collection_name}: nothing to change")
        continue

    patch_req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections/{current['id']}",
        data=json.dumps({"fields": new_fields}).encode(),
        headers={"Content-Type": "application/json", "Authorization": token},
        method="PATCH",
    )
    urllib.request.urlopen(patch_req)
    print(f"{collection_name}: patched\n")

print("Done. Your Go backend should now be able to store long reasoning/news")
print("text without hitting the 5000-character default limit.")
