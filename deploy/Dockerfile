# build -----------------------------------------------------------------------
FROM golang:1.26-alpine AS build

# vips-dev + C toolchain needed to link against libvips in the worker binary
RUN apk add --no-cache vips-dev gcc musl-dev

WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN go build -o /out/server ./cmd/server \
 && go build -o /out/worker ./cmd/worker

# server ----------------------------------------------------------------------
# The front service does not call vips; it proxies IIIF requests to Cantaloupe.
FROM alpine:3.20 AS server

ENV APP_HOME=/app

RUN apk add --no-cache ca-certificates \
 && mkdir -p /data/storage \
 && chown nobody:nobody /data/storage

WORKDIR $APP_HOME

# migrate binary runs migrations before the server starts (see entrypoint.sh).
COPY --from=migrate/migrate:v4.18.3 /migrate /usr/local/bin/migrate
COPY db/migrations $APP_HOME/migrations
COPY --from=build /out/server /usr/local/bin/server
COPY deploy/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

USER nobody
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# worker ----------------------------------------------------------------------
# The worker calls vips to build pyramidal TIFFs from uploaded scans.
FROM alpine:3.20 AS worker

RUN apk add --no-cache vips ca-certificates \
 && mkdir -p /data/storage \
 && chown nobody:nobody /data/storage

COPY --from=build /out/worker /usr/local/bin/worker

USER nobody
ENTRYPOINT ["/usr/local/bin/worker"]
