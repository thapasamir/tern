include .env
export

GOOSE = go run github.com/pressly/goose/v3/cmd/goose@v3.24.1
# sqlc runs from its official Docker image: pinned version, no local C toolchain needed
# (building it from source fails on recent macOS SDKs).
SQLC  = docker run --rm -v "$(CURDIR):/src" -w /src sqlc/sqlc:1.28.0

.PHONY: up down run worker build test vet sqlc sqlc-check migrate-up migrate-down migrate-status migrate-new

# Regenerate type-safe Go code from migrations/ and each feature's queries.sql.
sqlc:
	$(SQLC) generate

# Fails if generated code is stale, for CI: run `make sqlc-check`.
sqlc-check:
	$(SQLC) diff

up:
	docker compose -f ../docker-compose.yml up -d --wait

down:
	docker compose -f ../docker-compose.yml down

run:
	go run ./cmd/api

worker:
	go run ./cmd/worker

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker

test:
	go test ./...

vet:
	go vet ./...

migrate-up:
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" down

migrate-status:
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" status

# usage: make migrate-new name=create_tenants
migrate-new:
	$(GOOSE) -dir migrations create $(name) sql
