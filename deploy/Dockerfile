# build -----------------------------------------------------------------------
FROM golang:1.26-alpine AS build

# vips-dev + C toolchain needed to link against libvips in the worker binary
RUN apk add --no-cache vips-dev gcc musl-dev

WORKDIR /var/www/georef/image-service
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN go build -o /out/server ./cmd/server \
 && go build -o /out/worker ./cmd/worker

# server ----------------------------------------------------------------------
# The front service does not call vips; it proxies IIIF requests to Cantaloupe.
FROM alpine:3.20 AS server

RUN apk add --no-cache ca-certificates \
 && mkdir -p /data/storage \
 && chown nobody:nobody /data/storage

WORKDIR /var/www/georef/image-service
COPY --from=build /out/server /usr/local/bin/server

USER nobody
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]

# worker ----------------------------------------------------------------------
# The worker calls vips to build pyramidal TIFFs from uploaded scans.
FROM alpine:3.20 AS worker

RUN apk add --no-cache vips ca-certificates \
 && mkdir -p /data/storage \
 && chown nobody:nobody /data/storage

WORKDIR /var/www/georef/image-service
COPY --from=build /out/worker /usr/local/bin/worker

USER nobody
ENTRYPOINT ["/usr/local/bin/worker"]
