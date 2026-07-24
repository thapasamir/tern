include .env
export

GOOSE = go run github.com/pressly/goose/v3/cmd/goose@v3.24.1

.PHONY: up down run worker build test vet migrate-up migrate-down migrate-status migrate-new

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
