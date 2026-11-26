# End-to-end test plan

Manual test plan for the image service. Run through each section after starting
the stack with `docker compose up`.

Assumes the service is running at `http://localhost:8080` and GitHub auth is
disabled (`GITHUB_CLIENT_ID` not set).

---

## 1. Upload — happy path

### 1a. TIFF upload via the form
1. Open `http://localhost:8080` in a browser.
2. Fill in a title and select a valid TIFF file.
3. Click **Upload**.
4. Confirm the form shows "converting…" and starts polling.
5. Wait for conversion to finish — the form should display the IIIF base URL
   and manifest URL.

**Expected:** `202 Accepted` on submit; status transitions from `processing` →
`ready`; IIIF URLs appear.

### 1b. JPEG upload via the form
Repeat 1a with a JPEG file.

**Expected:** same outcome.

### 1c. Upload via API (curl)
```sh
curl -s -X POST http://localhost:8080/sheets \
  -F "title=Test scan" \
  -F "file=@/path/to/scan.tif" | jq .
```
**Expected:** `202` with `id`, `sheet_url`, `iiif_base_url`, `manifest_url`.

---

## 2. Sheet API

### 2a. List sheets
```sh
curl -s http://localhost:8080/sheets | jq .
```
**Expected:** array containing the uploaded sheet.

### 2b. Get sheet by ID
```sh
curl -s http://localhost:8080/sheets/{id} | jq .
```
**Expected:** sheet object with `status: "ready"` once conversion finishes.

### 2c. Update sheet metadata
```sh
curl -s -X PATCH http://localhost:8080/sheets/{id} \
  -H "Content-Type: application/json" \
  -d '{"title": "Updated title"}' | jq .
```
**Expected:** `200` with updated title.

### 2d. Delete sheet
```sh
curl -s -X DELETE http://localhost:8080/sheets/{id}
```
**Expected:** `204 No Content`.

```sh
curl -s http://localhost:8080/sheets/{id}
```
**Expected:** `410 Gone` (not 404).

---

## 3. IIIF serving

Run these after a sheet is `ready`.

### 3a. info.json
```sh
curl -s http://localhost:8080/iiif/{id}/info.json | jq .
```
**Expected:**
- `200` with `Content-Type: application/json`
- `@id` contains `localhost:8080/iiif/{id}` — not the internal Cantaloupe address
- `Cache-Control: public`

### 3b. Manifest
```sh
curl -s http://localhost:8080/iiif/{id}/manifest | jq .
```
**Expected:**
- `200` with a valid IIIF Presentation v3 manifest
- Canvas has correct `width` and `height`
- Painting annotation points at the IIIF image service

### 3c. Tile
```sh
curl -o tile.jpg http://localhost:8080/iiif/{id}/full/max/0/default.jpg
```
**Expected:** `200`, file is a readable JPEG.

---

## 4. Status routing

### 4a. Processing — 503
Upload a file and immediately (before conversion finishes) request info.json.

```sh
curl -I http://localhost:8080/iiif/{id}/info.json
```
**Expected:** `503 Service Unavailable` with `Retry-After` header.

### 4b. Failed sheet — 404 on IIIF
Upload a deliberately corrupt file (e.g. rename a `.txt` to `.tif`).
Wait for the worker to exhaust retries.

```sh
curl -s http://localhost:8080/sheets/{id} | jq .status
# → "failed"

curl -I http://localhost:8080/iiif/{id}/info.json
```
**Expected:** sheet API shows `status: "failed"` with an `error_message`;
IIIF returns `404`.

### 4c. Deleted sheet — 410
Delete a `ready` sheet (section 2d), then request its IIIF endpoints.

```sh
curl -I http://localhost:8080/iiif/{id}/info.json
curl -I http://localhost:8080/iiif/{id}/manifest
```
**Expected:** `410 Gone` on both.

### 4d. Unknown ID — 404
```sh
curl -I http://localhost:8080/iiif/doesnotexist/info.json
```
**Expected:** `404 Not Found`.

---

## 5. Worker retry

1. Upload a corrupt file and watch `docker compose logs worker`.
2. Confirm the worker retries up to `MAX_CONVERSION_RETRIES` (default 3) times.
3. After the last attempt, confirm `status` becomes `failed`.
4. Check `retry_count` on the sheet record via the API.

```sh
curl -s http://localhost:8080/sheets/{id} | jq '{status, retry_count, error_message}'
```

**Expected:** `retry_count: 3`, `status: "failed"`, `error_message` contains
the vips error.

---

## 6. File validation

### 6a. Wrong file type
```sh
curl -s -X POST http://localhost:8080/sheets \
  -F "title=Bad file" \
  -F "file=@/path/to/document.pdf" | jq .
```
**Expected:** `400 Bad Request` — unsupported file type.

### 6b. Missing title
```sh
curl -s -X POST http://localhost:8080/sheets \
  -F "file=@/path/to/scan.tif" | jq .
```
**Expected:** `400 Bad Request` — title is required.

### 6c. No file
```sh
curl -s -X POST http://localhost:8080/sheets \
  -F "title=No file" | jq .
```
**Expected:** `400 Bad Request`.

---

## 7. Write auth (when GitHub OAuth is enabled)

Skip this section if `GITHUB_CLIENT_ID` is not set — all writes pass through.

### 7a. Upload without a session
```sh
curl -s -X POST http://localhost:8080/sheets \
  -F "title=Test" \
  -F "file=@/path/to/scan.tif"
```
**Expected:** `401 Unauthorized`.

### 7b. Login flow
1. Visit `http://localhost:8080/auth/login`.
2. Approve on GitHub.
3. Confirm redirect back to `/`.
4. Re-run the upload — it should succeed.

### 7c. Logout
```sh
curl -s -X POST http://localhost:8080/auth/logout
```
**Expected:** `302` redirect to `/`; subsequent writes return `401`.

---

## 8. IIIF auth (when IIIF_AUTH_REQUIRED=true)

Set `IIIF_AUTH_REQUIRED=true` and restart the server.

### 8a. Anonymous IIIF request
```sh
curl -I http://localhost:8080/iiif/{id}/info.json
```
**Expected:** `401 Unauthorized`.

### 8b. Service credential bypass
```sh
curl -I http://localhost:8080/iiif/{id}/info.json \
  -H "Authorization: Bearer <SERVICE_CREDENTIAL>"
```
**Expected:** `200 OK` with `Cache-Control: private`.

### 8c. Cache-Control is public when IIIF auth is off
Disable `IIIF_AUTH_REQUIRED` and restart.

```sh
curl -I http://localhost:8080/iiif/{id}/info.json
```
**Expected:** `Cache-Control: public`.

---

## 9. Per-sheet visibility

Requires auth disabled (`GITHUB_CLIENT_ID` not set) or a valid write session.

### 9a. Upload a private sheet via the form
1. Open `http://localhost:8080`.
2. Fill in a title, choose a file, set **Visibility** to **Private**.
3. Upload and wait for `ready`.

**Expected:** sheet response includes `"public": false`.

### 9b. Upload a private sheet via API
```sh
curl -s -X POST http://localhost:8080/sheets \
  -F "title=Private scan" \
  -F "public=false" \
  -F "file=@/path/to/scan.tif" | jq .public
```
**Expected:** `false`.

### 9c. IIIF access denied for private sheet (no auth)
Using the ID from 9a or 9b:

```sh
curl -I http://localhost:8080/iiif/{id}/info.json
curl -I http://localhost:8080/iiif/{id}/manifest
```
**Expected:** `401 Unauthorized` on both, even when `IIIF_AUTH_REQUIRED=false`.

### 9d. Service credential grants access to private sheet
```sh
curl -I http://localhost:8080/iiif/{id}/info.json \
  -H "Authorization: Bearer <SERVICE_CREDENTIAL>"
```
**Expected:** `200 OK` with `Cache-Control: private`.

### 9e. Toggle visibility to public via PATCH
```sh
curl -s -X PATCH http://localhost:8080/sheets/{id} \
  -H "Content-Type: application/json" \
  -d '{"public": true}' | jq .public
```
**Expected:** `true`. PATCH is partial — only the fields sent are changed.

Confirm IIIF access is now open:
```sh
curl -I http://localhost:8080/iiif/{id}/info.json
```
**Expected:** `200 OK` with `Cache-Control: public`.

### 9f. Toggle back to private via PATCH
```sh
curl -s -X PATCH http://localhost:8080/sheets/{id} \
  -H "Content-Type: application/json" \
  -d '{"public": false}' | jq .public
```
**Expected:** `false`. IIIF returns `401` again.

### 9g. PATCH with public omitted leaves visibility unchanged
```sh
curl -s -X PATCH http://localhost:8080/sheets/{id} \
  -H "Content-Type: application/json" \
  -d '{"title": "Private scan"}' | jq .public
```
**Expected:** value unchanged from before the PATCH.

---

## 10. Sheet management page

Requires auth disabled or a valid write session.

### 10a. Open /manage
1. Navigate to `http://localhost:8080/manage`.

**Expected:** table listing sheets with title, status badge, public/private badge,
manifest link (for ready sheets), created date, and a toggle button.

### 10b. Toggle visibility from the UI
1. Find a public sheet in the table.
2. Click **Make private**.

**Expected:** badge changes to **private** and button label changes to **Make public**
without a page reload. Toast notification appears.

3. Click **Make public**.

**Expected:** badge reverts to **public**. Toast notification appears.

### 10c. Pagination
If more than the default limit of sheets exist:

**Expected:** Previous / Next links appear; clicking them loads the correct page of
results.

### 10d. Nav bar
1. From `/manage`, click **Upload** in the nav bar.

**Expected:** navigates to `/`.

2. From `/`, click **Manage sheets** in the nav bar.

**Expected:** navigates to `/manage`.

### 10e. /manage requires write auth (when GitHub OAuth is enabled)

Skip if `GITHUB_CLIENT_ID` is not set.

```sh
curl -I http://localhost:8080/manage
```
**Expected:** `302` redirect to `/auth/login`.

---

## 11. CORS

### 11a. Wildcard CORS (default)
```sh
curl -I http://localhost:8080/iiif/{id}/info.json \
  -H "Origin: https://example.com"
```
**Expected:** `Access-Control-Allow-Origin: *`.

### 11b. Specific origin allowed
Set `CORS_ALLOWED_ORIGINS=https://example.com` and restart.

```sh
curl -I http://localhost:8080/iiif/{id}/info.json \
  -H "Origin: https://example.com"
```
**Expected:** `Access-Control-Allow-Origin: https://example.com` and `Vary: Origin`.

### 11c. Origin not in allowlist
```sh
curl -I http://localhost:8080/iiif/{id}/info.json \
  -H "Origin: https://other.com"
```
**Expected:** no `Access-Control-Allow-Origin` header.

### 11d. Preflight handled before auth
Set `IIIF_AUTH_REQUIRED=true`. Send a preflight without credentials:

```sh
curl -I -X OPTIONS http://localhost:8080/iiif/{id}/info.json \
  -H "Origin: https://example.com" \
  -H "Access-Control-Request-Method: GET"
```
**Expected:** `200 OK` with CORS headers — no `401`.

---

## 12. Health and observability

### 12a. Server logs on upload
```sh
docker compose logs server
```
Confirm a structured JSON log line appears for the upload request.

### 12b. Worker logs on conversion
```sh
docker compose logs worker
```
Confirm log lines for each poll cycle and a success/failure line per sheet.

### 12c. Graceful shutdown
```sh
docker compose stop server
```
**Expected:** in-flight requests finish before the process exits; no abrupt
connection drops in the logs.

---

## 13. Checklist summary

| # | Test | Pass |
|---|---|---|
| 1a | TIFF upload via form | ☐ |
| 1b | JPEG upload via form | ☐ |
| 1c | Upload via curl | ☐ |
| 2a | List sheets | ☐ |
| 2b | Get sheet by ID | ☐ |
| 2c | Update metadata | ☐ |
| 2d | Delete → 410 | ☐ |
| 3a | info.json — @id rewrite + Cache-Control | ☐ |
| 3b | Manifest — correct dimensions | ☐ |
| 3c | Tile — readable JPEG | ☐ |
| 4a | Processing → 503 Retry-After | ☐ |
| 4b | Failed sheet → 404 on IIIF | ☐ |
| 4c | Deleted sheet → 410 | ☐ |
| 4d | Unknown ID → 404 | ☐ |
| 5 | Worker retry → failed after MAX_RETRIES | ☐ |
| 6a | Wrong file type → 400 | ☐ |
| 6b | Missing title → 400 | ☐ |
| 6c | No file → 400 | ☐ |
| 7a | Upload without session → 401 (when auth enabled) | ☐ |
| 7b | Login flow — OAuth redirect + session | ☐ |
| 7c | Logout → 401 on writes | ☐ |
| 8a | IIIF_AUTH_REQUIRED — anonymous IIIF → 401 | ☐ |
| 8b | Service credential → 200 + Cache-Control: private | ☐ |
| 8c | IIIF_AUTH_REQUIRED off → Cache-Control: public | ☐ |
| 9a | Upload private via form | ☐ |
| 9b | Upload private via API | ☐ |
| 9c | Private sheet IIIF — no auth → 401 | ☐ |
| 9d | Private sheet IIIF — service credential → 200 | ☐ |
| 9e | PATCH toggle to public → IIIF open | ☐ |
| 9f | PATCH toggle to private → IIIF 401 | ☐ |
| 9g | PATCH without public field — visibility unchanged | ☐ |
| 10a | /manage — sheet table loads | ☐ |
| 10b | Visibility toggle — badge and button update in place | ☐ |
| 10c | Pagination — prev/next links | ☐ |
| 10d | Nav bar — upload ↔ manage navigation | ☐ |
| 10e | /manage → 302 to login (when auth enabled) | ☐ |
| 11a | CORS — wildcard origin | ☐ |
| 11b | CORS — allowed specific origin + Vary | ☐ |
| 11c | CORS — unlisted origin → no header | ☐ |
| 11d | CORS preflight — 200 without auth | ☐ |
| 12 | Logs and graceful shutdown | ☐ |
