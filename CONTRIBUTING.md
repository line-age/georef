# Contributing

## Prerequisites

- Go 1.23 or later
- Docker (for integration tests and running the full stack locally)
- [`golangci-lint`](https://golangci-lint.run/usage/install/) for linting
- [`sqlc`](https://docs.sqlc.dev/en/latest/overview/install.html) for SQL code generation

## Running locally

```sh
docker compose up
```

This starts Postgres, the Cantaloupe image server, the front service, and the conversion worker.

## Makefile targets

| Target | What it does |
|---|---|
| `make build` | Compile both binaries to `bin/` |
| `make lint` | Run golangci-lint |
| `make test` | Run all tests |
| `make generate` | Re-run sqlc against `db/queries/` and `db/migrations/` |
| `make docker-build` | Build the Docker images |

## Code generation

SQL queries are compiled to type-safe Go by sqlc. After editing any `.sql` file in
`db/queries/` or `db/migrations/`, regenerate and commit both together:

```sh
make generate
git add db/ internal/db/
git commit -m "..."
```

## Package layout

Each `internal/` package owns one concern. Handlers in `internal/api/` receive
interfaces only; concrete types are injected in `cmd/server/main.go`. See
`CLAUDE.md` for the full layout and key invariants.
