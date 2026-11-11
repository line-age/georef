# CLAUDE.md — georef

Engineering context for agents working in this repo. Read this before writing any code.

## What this repo is

`georef` takes scanned maps in and puts IIIF images and georeference annotations out.
It runs Allmaps entirely self-hosted; nothing depends on allmaps.org at runtime.

Work is split across four issues: IIIF image service (#1), self-hosted Allmaps editor and
APIs (#2), upstream conversation with Allmaps (#4), and self-hosted Tile Server, Preview
and Viewer (#5). Each issue is independent; do not cross into another issue's scope unless
explicitly asked.

## Essential reading in this repo

- `docs/image-service.md` — design for the IIIF image service: parts, request routing,
  sheet lifecycle, proxy rules, read login, image server choice.
- `docs/background.md` — GIS, IIIF and Allmaps background for anyone new to these.
- `README.md` — full component architecture diagram covering all four issues.

## Tech stack

| Layer | Choice |
|---|---|
| Language | Go |
| Router | `go-chi/chi` v5 |
| Postgres driver | `jackc/pgx` v5 |
| Migrations | `golang-migrate/migrate` v4 |
| SQL codegen | `sqlc-dev/sqlc` |
| S3 storage | `aws/aws-sdk-go-v2` |
| OAuth | `golang.org/x/oauth2` |
| Sessions | `gorilla/sessions` |
| Linting | `golangci-lint` |
| Integration tests | `testcontainers-go` |
| Logging | `log/slog` (stdlib) |

## Key invariants

1. A sheet's IIIF URL never changes after upload — georeference annotations embed it permanently.
2. The original scan is never modified — the worker writes a separate pyramidal TIFF.
3. A deleted sheet ID is never reused — deleted sheets return 410, not 404.
4. Only `ready` sheets reach the image server — status routing lives in the front service.
5. The image server is internal only — never reachable from outside the cluster.
6. `info.json` always shows the public URL — the front service rewrites the image server's internal `@id`.
7. No environment values in this repo — credentials, team IDs and deployment config live outside.

## Target project layout

```
cmd/server/        HTTP server entry point
cmd/worker/        Conversion worker entry point
internal/domain/   Sheet entity, Status type — no I/O
internal/sheet/    SheetRepository interface + Postgres impl
internal/storage/  Store interface + filesystem impl + S3 impl
internal/convert/  vips wrapper (pyramidal TIFF only)
internal/iiif/     Manifest builder, info.json rewriter
internal/proxy/    Streaming proxy to image server
internal/auth/     GitHub OAuth, session middleware, team gate
internal/api/      HTTP handlers (depend on interfaces only)
db/migrations/     SQL migration files
db/queries/        .sql files for sqlc
docs/              Design and background docs
deploy/            Dockerfile, CI workflow
```

Handlers in `internal/api/` receive interfaces only — inject concrete types at startup.
The `internal/storage/` Store interface is the boundary between the filesystem and S3
backends; adding a backend touches no existing code.

## House rules (this is a public repo)

- No people's names, partner names, client names, specific map names or infrastructure
  details in any committed file.
- No environment values, secrets or deployment configuration in this repo.
- Comments and docs should be technical only.
