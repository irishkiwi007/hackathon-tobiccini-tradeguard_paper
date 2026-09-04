import json
import urllib.request

# --- Edit these to match your setup ---
EMAIL = "tobiccini@gmail.com"
PASSWORD = "1234567890"
# POCKETBASE_URL = "http://127.0.0.1:8090"
POCKETBASE_URL = "https://pocketbase-production-c181.up.railway.app"
IMPORT_FILE = "C:/Users/T/Desktop/Alpaca/tradeguard_paper/backend/tradeguard_collections_import.json"

# --- Auth ---
auth_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/_superusers/auth-with-password",
    data=json.dumps({"identity": EMAIL, "password": PASSWORD}).encode(),
    headers={"Content-Type": "application/json"},
)
token = json.load(urllib.request.urlopen(auth_req))["token"]
print("Authenticated.")

# --- Import ---
collections = json.load(open(IMPORT_FILE))
import_req = urllib.request.Request(
    f"{POCKETBASE_URL}/api/collections/import",
    data=json.dumps({"collections": collections, "deleteMissing": False}).encode(),
    headers={"Content-Type": "application/json", "Authorization": token},
    method="PUT",
)
resp = urllib.request.urlopen(import_req)
print(f"Import status: {resp.status}")
print("Done. Check http://127.0.0.1:8090/_/ to confirm all three collections")
print("now show 'created' and 'updated' fields.")
