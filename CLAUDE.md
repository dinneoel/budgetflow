# BudgetFlow — repo conventions

Monorepo: `backend/` (Go, chi + pgx + sqlc + PostgreSQL) and `frontend/` (React + Vite + TS). Commands run from the repo root via the Makefile: `make test`, `make test-backend`, `make test-frontend`, `make lint`, `make sqlc`, `make migrate-up`.

## Money

- All amounts are **int64 minor units** (cents/kopiykas). Never floats, never string math.
- Use `backend/internal/money` (Money type: currency-checked add/subtract, exact splits) and `backend/internal/budgetmath` (remaining/unallocated/status/rollover/goal math). Both are pure packages — no DB, no I/O — covered by table-driven tests. New financial calculations belong there, not inline in services.
- Splits must sum exactly to the parent amount; rounding remainders go to the earliest split legs.

## Database and sqlc workflow

- Migrations live in `backend/migrations/` as sequential four-digit pairs: `NNNN_name.up.sql` + `NNNN_name.down.sql`. Never renumber or edit an applied migration — add a new one. Every up needs a working down.
- Migrations are embedded via `go:embed` (`backend/migrations/migrations.go`), so tests and the server run them without caring about the working directory.
- sqlc reads its schema from `backend/migrations/` and query sources from `backend/internal/db/queries/*.sql` (one file per entity). After changing either, run `make sqlc`. Generated code lands in `backend/internal/db/` — never hand-edit generated files.
- All queries are user-scoped: filter by `user_id`, and cross-user access must 404/deny (tests assert this).
- Transactions use soft delete (`deleted_at`); accounts and categories use archive flags. Don't hard-delete except in full account deletion.

## Backend tests

- Backend tests run against **real PostgreSQL** — financial math is never mocked. `testdb.New(t)` (in `backend/internal/testdb`) creates a uniquely named throwaway database, migrates it up, and drops it on cleanup, so tests parallelize safely.
- The compose database must be up (`make db-up`). `TEST_DATABASE_URL` overrides the server (default: the docker-compose instance).
- `make test-backend` uses `go test -p 4` — package parallelism is bounded because each test opens its own DB and unbounded runs exhaust PostgreSQL `max_connections`. Keep that flag.
- `backend/internal/e2e` walks the 15 MVP acceptance criteria end-to-end; extend it when adding user-facing flows.

## Frontend layout

- `src/api/` — typed client (`client.ts` handles CSRF token, error normalization, auth-expiry redirect) plus one module per resource with TanStack Query hooks.
- `src/features/<area>/` — one folder per screen area (accounts, budget, transactions, …): page component, dialogs, a `use<Area>.ts` hook module, and colocated `*.test.tsx`.
- `src/components/` — shared UI only (layout, forms, `ModalDialog`, `EmptyState`). Use `ModalDialog` for any modal: it owns focus trap, Escape, and focus restore.
- Forms use react-hook-form + zod resolvers. Status is always conveyed by text + icon, never color alone; icon-only buttons need aria-labels. Key screens have axe-core checks — keep them passing.

## Other conventions

- Auth is session-cookie based (HttpOnly, SameSite) with CSRF tokens — no JWT. State-changing endpoints require the CSRF header.
- Mutating actions record audit events (`backend/internal/audit`); bulk operations record one event with affected IDs.
- Budget periods are restricted to the user's default currency; foreign-currency accounts are informational only — never silently convert.
- Email goes through the Mailer interface with a log-only dev implementation; don't wire SMTP directly into features.
