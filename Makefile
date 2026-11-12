BIN := bin

.PHONY: build lint test generate docker-build

build:
	go build -o $(BIN)/server ./cmd/server
	go build -o $(BIN)/worker ./cmd/worker

lint:
	golangci-lint run ./...

test:
	go test ./...

generate:
	docker run --rm -v $(PWD):/src -w /src sqlc/sqlc generate

docker-build:
	docker compose build
