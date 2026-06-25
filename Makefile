BINARY     = bin/server
MIGRATE    = migrate
MIGRATIONS = internal/db/migrations

# Load .env so DATABASE_URL is available without exporting it manually
ifneq (,$(wildcard .env))
  include .env
  export
endif

DB_URL ?= $(DATABASE_URL)

.PHONY: build run deps sqlc swagger \
        migrate-up migrate-down migrate-create migrate-force migrate-version \
        docker-up docker-down docker-db docker-logs \
        test lint clean

# ── Build ─────────────────────────────────────────────────────────────────────

## Compile the server binary to bin/server
build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/server

## Run locally (reads .env automatically)
run:
	go run ./cmd/server

## Download and tidy dependencies
deps:
	go mod tidy
	go mod download

# ── Code generation ───────────────────────────────────────────────────────────

## Regenerate SQLC query code from schema + queries
sqlc:
	sqlc generate

## Regenerate Swagger docs from handler annotations  →  make swagger
swagger:
	swag init -g cmd/server/main.go -o docs --parseInternal

# ── Migrations ────────────────────────────────────────────────────────────────

## Apply all pending migrations  →  make migrate-up
migrate-up:
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" up

## Short alias
migrate: migrate-up

## Roll back the most recent migration
migrate-down:
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" down 1

## Roll back ALL migrations (full wipe — use carefully)
migrate-reset:
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" down -all

## Create a new migration file pair:  make migrate-create name=add_cards_table
migrate-create:
	$(MIGRATE) create -ext sql -dir $(MIGRATIONS) -seq $(name)

## Force the schema version without running SQL (use after a manual schema load)
##   make migrate-force v=1
migrate-force:
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" force $(v)

## Print the current migration version
migrate-version:
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" version

# ── Docker ────────────────────────────────────────────────────────────────────

## Start postgres + redis only (recommended for local dev)
docker-db:
	docker compose up -d db redis

## Start all services (db, redis, app)
docker-up:
	docker compose up -d

## Stop and remove containers (data volume is preserved)
docker-down:
	docker compose down

## Tail logs for db and redis
docker-logs:
	docker compose logs -f db redis

# ── Quality ───────────────────────────────────────────────────────────────────

## Run tests with race detector and coverage
test:
	go test -race -cover ./...

## Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

# ── Housekeeping ──────────────────────────────────────────────────────────────

## Remove compiled binaries
clean:
	rm -rf bin/

.DEFAULT_GOAL := build
