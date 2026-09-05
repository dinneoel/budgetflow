# BudgetFlow

A personal budgeting app: zero-based monthly budgets, accounts, transactions, recurring bills, savings goals, CSV import/export, and reports.

## Architecture

- `backend/` — Go API server: [chi](https://github.com/go-chi/chi) router, PostgreSQL via pgx + [sqlc](https://sqlc.dev), golang-migrate migrations. Money is stored as int64 minor units (cents) — never floats.
- `frontend/` — React + TypeScript (Vite): React Router, TanStack Query, Tailwind CSS. Tests with Vitest + React Testing Library.
- `docker-compose.yml` — PostgreSQL 16 with `budgetflow_dev` and `budgetflow_test` databases.

## Local setup

Requirements: Go 1.26+, Node 24+, Docker with the compose plugin.

```sh
# start PostgreSQL (creates dev + test databases on first run)
make db-up

# apply migrations
make migrate-up

# run backend (:8080) and frontend (:5173, proxies /api to the backend)
make dev
```

Backend configuration is environment-based: `PORT`, `DATABASE_URL`, `SESSION_SECRET`, `APP_ENV` (`dev`/`test`/`prod`). Dev defaults work with the compose database out of the box.

## Test and lint

```sh
make test            # backend + frontend
make test-backend    # go test ./...
make test-frontend   # vitest run
make lint            # go vet + eslint
```

Backend integration tests run against the real `budgetflow_test` database (`TEST_DATABASE_URL` to override).

## Other targets

```sh
make sqlc          # regenerate typed queries from backend/internal/db/queries
make migrate-down  # roll back one migration
make db-down       # stop PostgreSQL
```
