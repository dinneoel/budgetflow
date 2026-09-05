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

# The migrate CLI picks its driver from the URL scheme; we use the pgx/v5 driver.
MIGRATE = go run -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate
MIGRATE_URL = $(subst postgres://,pgx5://,$(DATABASE_URL))

migrate-up:
	cd backend && $(MIGRATE) -path migrations -database "$(MIGRATE_URL)" up

migrate-down:
	cd backend && $(MIGRATE) -path migrations -database "$(MIGRATE_URL)" down 1

sqlc:
	cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc generate
