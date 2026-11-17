#!/bin/sh
set -e
# Build the postgres DSN from individual DB_* vars (the Go binaries use the same vars).
DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT:-5432}/${DB_NAME}?sslmode=${DB_SSLMODE:-disable}"
migrate -path "$APP_HOME/migrations" -database "$DATABASE_URL" up
exec /usr/local/bin/server
