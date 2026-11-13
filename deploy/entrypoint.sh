#!/bin/sh
set -e
migrate -path "$APP_HOME/migrations" -database "$DATABASE_URL" up
exec /usr/local/bin/server
