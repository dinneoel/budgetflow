# Optional machine-local overrides (Go cache paths, tool locations); not committed.
-include local.mk

.PHONY: dev dev-backend dev-frontend test test-backend test-frontend lint lint-backend lint-frontend db-up db-down migrate-up migrate-down sqlc

dev: db-up
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	cd backend && go run ./cmd/server

dev-frontend:
	cd frontend && npm run dev

test: test-backend test-frontend

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm test

lint: lint-backend lint-frontend

lint-backend:
	cd backend && go vet ./...

lint-frontend:
	cd frontend && npm run lint

db-up:
	docker compose up -d --wait db

db-down:
	docker compose down

TEST_DATABASE_URL ?= postgres://budgetflow:budgetflow@localhost:5432/budgetflow_test?sslmode=disable
DATABASE_URL ?= postgres://budgetflow:budgetflow@localhost:5432/budgetflow_dev?sslmode=disable

migrate-up:
	cd backend && go run github.com/golang-migrate/migrate/v4/cmd/migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	cd backend && go run github.com/golang-migrate/migrate/v4/cmd/migrate -path migrations -database "$(DATABASE_URL)" down 1

sqlc:
	cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc generate
