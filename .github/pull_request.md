# feat(image-service): implement IIIF image service (#1)

## Summary

Implements the full IIIF image service described in issue #1. The service takes
scanned maps in (JPEG or TIFF) and puts IIIF images and georeference annotations
out — entirely self-hosted, with no runtime dependency on allmaps.org.

This PR adds everything from the initial scaffold to a production-ready service:
upload pipeline, conversion worker, IIIF proxy, GitHub OAuth, S3 storage, and
the full supporting infrastructure (CI, Dockerfile, Helm chart, migrations,
OpenAPI spec, and documentation).

---

## What was built

### Upload pipeline

A single `POST /sheets` endpoint accepts JPEG or TIFF scans up to 1 GB (default).
The server streams the upload directly to storage in one pass, computing the
SHA-256 checksum and byte count without buffering the file in memory. The sheet
is inserted with `status = 'processing'` and the client receives `202 Accepted`
with a stable IIIF base URL and manifest URL immediately — before conversion
starts.

Key commits:
- `49796d8` — sheet CRUD and IIIF proxy endpoints
- `c703840` — `OriginalScanPath` added to `Store` interface
- `9b006f8` — TIFF content-type detection via magic bytes (not file extension)

### Conversion worker

A separate worker process polls Postgres using `SELECT … FOR UPDATE SKIP LOCKED`,
so the `sheets` table is the job queue with no additional infrastructure. The
worker streams the original scan to a local temp file (required because vips
cannot read `s3://` URIs), runs `vips tiffsave --tile --pyramid`, then streams
the result to storage. Conversion time is logged as `convert_ms` on every
successful job.

Key commits:
- `b9a3f2d` — worker package
- `38bd077` — bounded retry; `MAX_CONVERSION_RETRIES` wired end-to-end
- `ea914e9` — S3 fix: stream through `OpenOriginalScan` to a local temp file
  before calling vips (vips cannot read `s3://` paths directly)

### IIIF proxy and status routing

Every IIIF request (`/iiif/{id}/info.json`, tiles, manifest) goes through a
status check before anything reaches the image server:

| Sheet state | Response |
|---|---|
| Unknown ID | `404 Not Found` |
| Deleted | `410 Gone` (ID never reused) |
| `processing` | `503 Service Unavailable` + `Retry-After` |
| `failed` | `404 Not Found` |
| `ready` | Streamed from Cantaloupe |

The `info.json` proxy rewrites the `@id` field from Cantaloupe's internal address
to the public URL. Tile responses are streamed without buffering. A TTL cache
(default 5 s) covers the status lookup so a viewer panning across dozens of tiles
does not hit Postgres on every request.

Key commits:
- `da9c756` — streaming reverse proxy
- `4c8193b` — IIIF Presentation v3 manifest builder
- `2fe211c` — TTL cache for sheet status lookups
- `ea914e9` — `Cache-Control: public` / `private` based on `READ_LOGIN_REQUIRED`

### GitHub OAuth and team-gate middleware

Write endpoints require a GitHub session in an allowed team
(`GITHUB_ALLOWED_TEAMS`). The OAuth flow uses CSRF-safe state nonces stored in
a signed cookie session. Team membership is checked once at login via the GitHub
teams API (paginated); subsequent requests check only the session flag.

A `READ_LOGIN_REQUIRED` switch gates IIIF reads as well. When on, every IIIF
request must present either a GitHub session or an `Authorization: Bearer
<SERVICE_CREDENTIAL>` header (for server-side callers such as the Allmaps Tile
Server that cannot go through a browser login). The token comparison uses
`crypto/subtle.ConstantTimeCompare` to prevent timing attacks.

Set `AUTH_REQUIRED=false` to bypass all auth middleware without removing GitHub
config — useful for local testing.

Key commits:
- `c57670c` — GitHub OAuth and team-gate middleware
- `ea914e9` — `RequireRead` and service credential
- `70b1028` — `AUTH_REQUIRED` toggle

### S3 storage backend

A switchable storage backend (`STORAGE_BACKEND=filesystem` or `s3`). The S3
backend uses `aws-sdk-go-v2` with `feature/s3/manager` for streaming uploads and
`UsePathStyle=true` for MinIO compatibility. Credentials come from the standard
AWS credential chain. A MinIO testcontainer integration test covers all `Store`
interface methods.

Key commits:
- `ea914e9` — S3 storage backend and integration test

### Upload form with progress

A minimal browser upload form at `GET /` renders via Go's `html/template`. It
uses `XMLHttpRequest` (not `fetch`) so it can listen to `upload.progress` events
and show a live progress bar during upload. After upload it polls
`GET /sheets/{id}` every 3 seconds until the sheet reaches `ready` or `failed`.

Key commits:
- `576ad5f` — web upload form
- `70b1028` — XHR progress bar

### Interactive API docs

A Swagger UI page at `GET /docs` lets you browse and try every endpoint in the
browser without any extra tooling. The raw OpenAPI spec is at
`GET /docs/openapi.yaml`. The spec is embedded in the binary at build time so
there are no external files to deploy. Endpoints are grouped into four
categories — Web, Auth, Sheets, and IIIF — to make it easy to find what you
need.

Key commits:
- `02db723` — Swagger UI served at `/docs`; spec embedded with `//go:embed`
- `e374131` — naming and no-hardcoding rules applied to docs handlers
- `4affb51` — OpenAPI tags to separate Web, Auth, Sheets, and IIIF endpoints
- `7899e6c` — `/docs` and `/docs/openapi.yaml` added to spec and request table

### IIIF version handling

The service uses two independent IIIF specs that version separately:

- **Presentation API v3** — the manifest format. Fixed at v3 because Allmaps
  requires it. Changing this needs a code edit.
- **Image API v2 (default)** — tiles and `info.json`, served by Cantaloupe.
  Controlled by the `IIIF_IMAGE_API_VERSION` config. Can be switched to v3
  without any code change.

Previously the manifest always embedded Image API v2 service descriptor JSON
keys (`@id`, `@type`) regardless of configuration. The service now selects the
correct key names and profile format for whichever Image API version is in use:
v2 uses `@id`/`@type` and a full profile URL; v3 uses `id`/`type` and a plain
level string.

Key commits:
- `183cb83` — version-correct JSON tags in the manifest service descriptor
- `03a90ce` — cleaned up redundant parameter from the helper function
- `b79dc96` — `.env.example` and `docs/iiif-serving.md` updated with version table
- `0814a91` — plain-language version explanation added to `docs/iiif-serving.md`

### Infrastructure

- **CI** (`5d7427f`) — lint (`golangci-lint v2`), unit tests, and Docker image
  build on every push
- **Dockerfile** (`8da8560`, `deploy/Dockerfile`) — multi-stage build; server
  and worker as separate targets; `entrypoint.sh` runs migrations before the
  server starts
- **Helm chart** (`6165946`) — `deploy/chart/` with server, worker, Cantaloupe,
  ingress, and persistent volume templates
- **Migrations** — three sequential migrations: `001` creates `sheets`,
  `002` adds `retry_count`, `003` fixes the claimable index to `processing`-only
  rows

---

## Configuration

All tuneable values are environment variables with documented defaults. See
`.env.example` for the full reference. Key variables:

| Variable | Default | Purpose |
|---|---|---|
| `AUTH_REQUIRED` | `true` | Set `false` to bypass auth without removing GitHub config |
| `GITHUB_CLIENT_ID` | — | Omit to disable auth entirely |
| `GITHUB_ALLOWED_TEAMS` | — | Comma-separated `org/team` slugs |
| `READ_LOGIN_REQUIRED` | `false` | Gate IIIF reads behind a session |
| `SERVICE_CREDENTIAL` | — | Bearer token for server-side IIIF callers |
| `STORAGE_BACKEND` | `filesystem` | `filesystem` or `s3` |
| `MAX_UPLOAD_SIZE` | `1073741824` | 1 GB |
| `MAX_CONVERSION_RETRIES` | `3` | Worker retry budget per sheet |
| `IIIF_STATUS_CACHE_TTL` | `5s` | Status lookup cache lifetime |
| `RETRY_AFTER_SECONDS` | `5` | Value of the `Retry-After` header on 503 |

---

## Documentation

| Doc | What it covers |
|---|---|
| `docs/auth.md` | OAuth flow, session design, `RequireWrite`, `RequireRead`, setup guide |
| `docs/flows.md` | Upload, worker, retry, auth, and form flows with Mermaid diagrams |
| `docs/iiif-serving.md` | Status routing, proxy, manifest build, `Cache-Control` |
| `docs/model.md` | Data model, column reference, design decisions |
| `docs/image-service.md` | Architecture overview and design rationale |
| `docs/openapi.yaml` | Full OpenAPI 3.1 spec for all endpoints |
| `docs/test-plan.md` | Manual end-to-end test plan |

---

## Test plan

See `docs/test-plan.md` for the full manual test checklist. Automated coverage:

- `internal/api/upload_test.go` — upload validation, size limits, MIME checks
- `internal/api/sheets_test.go` — sheet CRUD, delete → 410 on IIIF
- `internal/auth/github_test.go` — auth middleware: passthrough when disabled, 401 when enabled (`e9514cb`)
- `internal/worker/worker_test.go` — conversion, retry, exhaustion
- `internal/sheet/sheet_test.go` — Postgres integration via testcontainers
- `internal/storage/s3_test.go` — S3/MinIO integration via testcontainers

---

## Key invariants maintained

1. A sheet's IIIF URL never changes after upload.
2. The original scan is never modified — the worker writes a separate pyramidal TIFF.
3. A deleted sheet ID is never reused — deleted sheets return `410`, not `404`.
4. Only `ready` sheets reach the image server.
5. The image server is internal only — no `ports:` mapping in docker-compose.
6. `info.json` always shows the public URL — the proxy rewrites the internal `@id`.
7. No environment values in this repo — credentials and deployment config live outside.

---

🤖 Generated with [Claude Code](https://claude.com/claude-code)
