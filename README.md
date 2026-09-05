# BudgetFlow

A personal budgeting app: zero-based monthly budgets, accounts, transactions, recurring bills, savings goals, in-app notifications, CSV import/export, dashboard and reports, and full data export with account deletion.

## Features

- **Zero-based budgeting** — monthly budget periods with per-category allocations, "Available to assign" tracking, rollover rules (none / roll over / reset to target), and allocation history.
- **Accounts** — manual accounts (cash, checking, savings, credit card, e-wallet, custom) with computed balances, reconciliation, archiving, and a net-worth inclusion flag.
- **Transactions** — income, expense, refund, adjustment, and paired transfers; splits across categories; filters, search, and pagination; bulk categorize/tag/delete/review; duplicate warnings; soft delete with restore.
- **Recurring bills** — weekly/monthly/annual/custom rules with month-end clamping, mark-as-paid, match-to-transaction, and upcoming-bill reserves in category math.
- **Goals** — savings/payoff/purchase goals with contributions, required monthly contribution, and behind-schedule detection.
- **Notifications** — in-app alerts for threshold/over-budget categories, bills due, missing budget month, and goals behind schedule, with per-type preferences and dedupe.
- **CSV import/export** — upload → column mapping → preview with per-row errors and duplicate flags → atomic commit; batch undo; CSV exports per entity and a full-data ZIP export.
- **Security** — session-cookie auth (HttpOnly, SameSite) with CSRF protection, argon2id password hashing, rate limiting and lockout, audit events, and re-authenticated account deletion.

## Architecture

- `backend/` — Go API server: [chi](https://github.com/go-chi/chi) router, PostgreSQL via pgx + [sqlc](https://sqlc.dev), golang-migrate migrations. Money is stored as int64 minor units (cents) — never floats. Budget/money math lives in pure packages (`internal/money`, `internal/budgetmath`) with table-driven tests.
- `frontend/` — React + TypeScript (Vite): React Router, TanStack Query, Tailwind CSS, react-hook-form + zod. Tests with Vitest + React Testing Library, including axe-core accessibility checks.
- `docker-compose.yml` — PostgreSQL 16 with `budgetflow_dev` and `budgetflow_test` databases.

```
backend/
  cmd/server/          entry point
  migrations/          numbered SQL migrations (embedded via go:embed)
  internal/
    db/                sqlc-generated code + queries/*.sql sources
    money/ budgetmath/ pure calculation packages
    auth/ accounts/ categories/ budgets/ transactions/ recurring/
    goals/ notifications/ importer/ exporter/ reports/ users/
    httpserver/        router, middleware (sessions, CSRF, rate limit)
    testdb/            per-test throwaway database provisioning
    e2e/               end-to-end acceptance-criteria walk
frontend/src/
  api/                 typed API client, one module per resource
  components/          shared UI (layout, forms, ModalDialog, EmptyState)
  features/            one folder per screen area (budget, transactions, …)
  routes/              route definitions and guards
```

## Local setup

Requirements: Go 1.26+, Node 24+, Docker with the compose plugin.

```sh
# start PostgreSQL (creates dev + test databases on first run)
make db-up

# apply migrations to the dev database
make migrate-up

# run backend (:8080) and frontend (:5173, proxies /api to the backend)
make dev
```

Then install frontend dependencies once with `cd frontend && npm install` if you haven't already.

Backend configuration is environment-based: `PORT`, `DATABASE_URL`, `SESSION_SECRET`, `APP_ENV` (`dev`/`test`/`prod`). Dev defaults work with the compose database out of the box; `SESSION_SECRET` is required only when `APP_ENV=prod`. Password-reset email is delivered via a log-only dev mailer; production SMTP is not wired up.

## Test and lint

```sh
make test            # backend + frontend
make test-backend    # go test -p 4 ./...  (bounded so parallel test DBs fit in max_connections)
make test-frontend   # vitest run
make lint            # go vet + eslint
```

Backend integration tests run against a real PostgreSQL server: each test provisions its own throwaway database (see `backend/internal/testdb`), so `make db-up` must be running. Set `TEST_DATABASE_URL` to point tests at a different server.

## Other targets

```sh
make sqlc          # regenerate typed queries from backend/internal/db/queries
make migrate-down  # roll back one migration
make db-down       # stop PostgreSQL
```
