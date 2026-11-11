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
	sqlc generate

docker-build:
	docker compose build
