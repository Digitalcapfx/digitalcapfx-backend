BINARY     = bin/server
MIGRATE    = migrate
DB_URL    ?= $(DATABASE_URL)

.PHONY: build run deps sqlc \
        migrate migrate-down migrate-create \
        docker-up docker-down docker-build \
        test lint clean

## Build the binary
build:
	@mkdir -p bin
	@go build -o $(BINARY) ./cmd/server

## Run locally (requires .env)
run:
	@go run ./cmd/server

## Download and tidy dependencies
deps:
	@go mod tidy
	@go mod download

## Generate SQLC code from queries + schema
sqlc:
	@sqlc generate

## Run all pending migrations
migrate:
	@$(MIGRATE) -path internal/db/migrations -database "$(DB_URL)" up

## Roll back one migration
migrate-down:
	@$(MIGRATE) -path internal/db/migrations -database "$(DB_URL)" down 1

## Create a new migration: make migrate-create name=add_cards_table
migrate-create:
	@$(MIGRATE) create -ext sql -dir internal/db/migrations -seq $(name)

## Start local services (postgres + redis) in background
docker-up:
	@docker compose up -d

## Stop and remove local services
docker-down:
	@docker compose down

## Build the Docker image
docker-build:
	@docker build -t digitalfx:local .

## Run tests
test:
	@go test -race -cover ./...

## Run linter (requires golangci-lint)
lint:
	@golangci-lint run ./...

## Remove built binaries
clean:
	@rm -rf bin/

.DEFAULT_GOAL := build
