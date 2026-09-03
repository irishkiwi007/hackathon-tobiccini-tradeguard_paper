import json
import urllib.request
import urllib.error

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


def get_collection(name):
    req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections/{name}",
        headers={"Authorization": token},
    )
    try:
        return json.load(urllib.request.urlopen(req))
    except urllib.error.HTTPError as e:
        if e.code == 404:
            return None
        raise


# --- 1. Add bull_case / bear_case to proposed_trades ---
trades = get_collection("proposed_trades")
if trades is None:
    print("proposed_trades: NOT FOUND — is your instance set up at all?")
else:
    field_names = [f["name"] for f in trades["fields"]]
    new_fields = list(trades["fields"])
    added = []
    for name in ("bull_case", "bear_case"):
        if name not in field_names:
            new_fields.append({"name": name, "type": "text", "max": 50000})
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
        print("proposed_trades: bull_case/bear_case already present, nothing to do")

# --- 2. Create system_config if missing, and seed one record ---
config = get_collection("system_config")
if config is None:
    create_req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections",
        data=json.dumps({
            "name": "system_config",
            "type": "base",
            "fields": [
                {"name": "paused", "type": "bool"},
                {"name": "created", "type": "autodate", "onCreate": True, "onUpdate": False},
                {"name": "updated", "type": "autodate", "onCreate": True, "onUpdate": True},
            ],
            "listRule": "", "viewRule": "", "updateRule": "", "createRule": None,
        }).encode(),
        headers={"Content-Type": "application/json", "Authorization": token},
        method="POST",
    )
    urllib.request.urlopen(create_req)
    print("system_config: collection created")
else:
    print("system_config: collection already exists")

# Seed a record if none exists yet (works whether the collection was
# just created above or already existed but was never seeded).
list_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/system_config/records?perPage=1",
    headers={"Authorization": token},
)
existing = json.load(urllib.request.urlopen(list_req))
if existing["items"]:
    print(f"system_config: record already exists (paused={existing['items'][0]['paused']})")
else:
    seed_req = urllib.request.Request(
        f"{POCKETBASE_URL}/api/collections/system_config/records",
        data=json.dumps({"paused": False}).encode(),
        headers={"Content-Type": "application/json", "Authorization": token},
        method="POST",
    )
    urllib.request.urlopen(seed_req)
    print("system_config: seeded initial record (paused=false)")

print("\nDone. Restart your Go server to pick this up.")
