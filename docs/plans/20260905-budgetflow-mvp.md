# BudgetFlow MVP: Full Implementation Plan (Phases 1 + 2)

## Overview
Bootstrap the complete BudgetFlow MVP from an empty repository: a Go backend (chi + sqlc + PostgreSQL) and a React (Vite + TypeScript) frontend, covering all 15 PRD acceptance criteria — auth/profile, accounts, categories, monthly budgets with rollover, transactions (all types, splits, bulk ops), recurring bills, savings goals, in-app notifications, CSV import/export, dashboard/reports, account deletion with full data export, and accessible responsive UI.

## Context
  - Files involved: greenfield — repository contains only `readme.md`
  - Repo layout: monorepo with `backend/` (Go) and `frontend/` (React + Vite + TS), `docker-compose.yml` for PostgreSQL, `Makefile` for dev/test/lint commands
  - Dependencies (backend): chi (router), pgx (driver), sqlc (query codegen), golang-migrate (migrations), argon2id password hashing, gorilla/csrf or equivalent middleware
  - Dependencies (frontend): React Router, TanStack Query, Tailwind CSS, Vitest + React Testing Library, react-hook-form + zod
  - Key decisions baked into this plan (changeable on review):
  - Zero-based "available to allocate" budgeting model, per the PRD budget workspace spec
  - Money stored as int64 minor units (cents/kopiykas) — never floats
  - Session-cookie auth (HttpOnly, SameSite) rather than JWT; CSRF token protection
  - MVP restricts each budget period to the user's default currency; accounts in other currencies show informational balances only, no silent conversion
  - Email delivery (password reset, notifications) behind an interface with a dev/log implementation; SMTP wiring deferred
  - Soft delete for transactions (restore support); archive flags for accounts/categories

## Development Approach
  - **Testing approach**: Regular (code first, then tests within the same task)
  - Backend tests run against a real PostgreSQL instance (docker-compose) — financial math must not be mocked away
  - The budget/money calculation engine is a pure Go package with exhaustive table-driven tests
  - Complete each task fully before moving to the next
  - **CRITICAL: every task MUST include new/updated tests**
  - **CRITICAL: all tests must pass before starting next task**

## Implementation Steps

### Task 1: Repository scaffolding and dev environment
**Files:**
  - Create: `docker-compose.yml`, `Makefile`, `.gitignore`, `README.md` (replace `readme.md`)
  - Create: `backend/go.mod`, `backend/cmd/server/main.go`, `backend/internal/config/config.go`, `backend/internal/httpserver/server.go`
  - Create: `frontend/` via Vite React-TS template, plus Tailwind, ESLint/Prettier, Vitest config
  - [x] initialize Go module with chi server skeleton, health endpoint `/api/health`, graceful shutdown, env-based config (DB URL, port, session secret)
  - [x] add docker-compose with PostgreSQL 16 (dev + test databases)
  - [x] scaffold Vite React-TS app with Tailwind, React Router, TanStack Query, Vitest + RTL configured
  - [x] add Makefile targets: `dev`, `test`, `test-backend`, `test-frontend`, `lint`, `migrate-up`, `sqlc`
  - [x] write tests: Go health-endpoint test; one frontend smoke render test
  - [x] run project test suite - must pass before task 2

### Task 2: Database schema, migrations, and sqlc setup
**Files:**
  - Create: `backend/migrations/0001_init.up.sql` / `.down.sql` (and subsequent numbered migrations)
  - Create: `backend/sqlc.yaml`, `backend/internal/db/queries/*.sql`, generated `backend/internal/db/`
  - [x] write migrations for all core entities: users, sessions, password_reset_tokens, accounts, category_groups, categories, budget_periods, budget_allocations, allocation_history, transactions, transaction_splits, tags, transaction_tags, recurring_rules, goals, goal_contributions, import_batches, notifications, notification_preferences, audit_events
  - [x] enforce invariants in schema: amounts as BIGINT minor units, FK constraints with user scoping, unique (user_id, year, month) budget period, check constraints on transaction types and statuses, soft-delete column on transactions
  - [x] configure sqlc and write initial query files per entity; generate typed Go code
  - [x] add test helper that provisions a clean test database (migrate up/down) per test package
  - [x] write tests: migration up/down round-trip; constraint violation checks (duplicate period, orphaned split)
  - [x] run project test suite - must pass before task 3

### Task 3: Money and budget-math engine (pure package)
**Files:**
  - Create: `backend/internal/money/money.go`, `backend/internal/budgetmath/budgetmath.go` + tests
  - [x] implement Money type over int64 minor units with currency code, safe add/subtract/split, and formatting metadata
  - [x] implement category math: `Remaining = Budgeted + Rollover − ActualSpending − ReservedUpcomingPayments`
  - [x] implement period math: `Unallocated = AvailableIncome + PriorRollover − Σ Allocations`
  - [x] implement status classification (on track / approaching limit / over budget / unfunded) with configurable threshold (default 20%)
  - [x] implement rollover rules: none, roll over unused balance, reset to target
  - [x] implement goal math: required monthly contribution and behind-schedule detection
  - [x] write exhaustive table-driven tests including negative amounts, zero budgets, refunds, and split rounding (splits must sum exactly to the parent amount)
  - [x] run project test suite - must pass before task 4

### Task 4: Authentication, sessions, profile, and security baseline
**Files:**
  - Create: `backend/internal/auth/` (handlers, service, password hashing), `backend/internal/httpserver/middleware/` (sessions, CSRF, rate limit, request logging), `backend/internal/audit/audit.go`
  - [x] implement sign-up (email/password, argon2id), sign-in, sign-out, sign-out-all-devices with DB-backed sessions and HttpOnly/SameSite cookies
  - [x] implement password reset flow: token issue + consume, delivery via a Mailer interface with a log-only dev implementation
  - [x] implement profile endpoints: name, locale, time zone, first day of week, default currency
  - [x] add middleware: authentication guard, CSRF protection, per-IP and per-account rate limiting, account lockout after repeated failures
  - [x] record audit events for sign-in, sign-out, password change/reset, profile changes
  - [x] write tests: full auth flow, lockout behavior, CSRF rejection, session invalidation, audit rows written
  - [x] run project test suite - must pass before task 5

### Task 5: Accounts API
**Files:**
  - Create: `backend/internal/accounts/` (handlers, service), queries in `backend/internal/db/queries/accounts.sql`
  - [x] CRUD for manual accounts: name, institution, type (cash/checking/savings/credit_card/ewallet/custom), currency, opening balance, net-worth inclusion flag
  - [x] archive/unarchive without touching historical transactions
  - [x] current balance computed from opening balance + transactions (single source of truth, no stored drift)
  - [x] reconciliation endpoint: compare statement balance, create adjustment transaction for the difference
  - [x] write tests: balance computation across transaction types, archive behavior, reconciliation adjustment, cross-user access denied
  - [x] run project test suite - must pass before task 6

### Task 6: Categories and category groups API
**Files:**
  - Create: `backend/internal/categories/`, queries in `backend/internal/db/queries/categories.sql`
  - [x] CRUD + reorder for category groups and categories; icon and color fields
  - [x] category budget type (fixed, variable, sinking fund, debt, savings-goal) and rollover rule per category
  - [x] archive and merge (merge reassigns transactions and allocations, records audit event)
  - [x] seed default groups/categories from the PRD list on first budget creation or via explicit endpoint
  - [x] write tests: reorder persistence, merge correctness (totals preserved), archive with existing transactions
  - [x] run project test suite - must pass before task 7

### Task 7: Budget periods and allocations API
**Files:**
  - Create: `backend/internal/budgets/`, queries in `backend/internal/db/queries/budgets.sql`
  - [x] create monthly budget period in user's default currency; copy prior month's structure and allocations
  - [x] set/update planned income and per-category allocations; every change appended to allocation_history
  - [x] compute and return unallocated funds and per-category remaining/status using the budgetmath package
  - [x] apply rollover on period creation according to each category's rollover rule
  - [x] budget notes field per period
  - [x] write tests: copy-prior-month, unallocated math, rollover across two periods, history recorded on every change
  - [x] run project test suite - must pass before task 8

### Task 8: Transactions API (all types, splits, bulk, duplicates)
**Files:**
  - Create: `backend/internal/transactions/`, queries in `backend/internal/db/queries/transactions.sql`
  - [x] create/edit/duplicate/soft-delete/restore for income, expense, refund, adjustment transactions; category required except for transfers
  - [x] transfers as paired ledger entries between accounts, excluded from spending analytics
  - [x] splits: allocate one transaction across 2+ categories, amounts must sum to the parent
  - [x] filters (date range, account, category, payee, amount range, status, type, tags) and text search (payee, notes, amount); pagination
  - [x] bulk operations: categorize, tag, delete, mark reviewed — each recorded as one audit event with affected IDs
  - [x] duplicate detection on entry: same account, amount, date ±1 day, similar payee → warning flag in response
  - [x] write tests: each type's effect on account balance and category spending, split sum enforcement, transfer exclusion from analytics, restore correctness, bulk ops, duplicate flagging
  - [x] run project test suite - must pass before task 9

### Task 9: Recurring bills and subscriptions API
**Files:**
  - Create: `backend/internal/recurring/`, queries in `backend/internal/db/queries/recurring.sql`
  - [x] CRUD for recurring rules: frequency (weekly, monthly, annual, custom interval), expected amount, due date, account, category, reminder lead time
  - [x] next-due-date computation (handles month-end clamping, e.g. due on the 31st)
  - [x] mark-as-paid: creates the real transaction and advances next due date; manual match to an existing transaction
  - [x] upcoming-bills endpoint (next N days) feeding dashboard and reserved-upcoming-payments in category math
  - [x] write tests: schedule advancement across frequencies and month-end edge cases, mark-paid transaction creation, reserved amount appears in category remaining
  - [x] run project test suite - must pass before task 10

### Task 10: Goals API
**Files:**
  - Create: `backend/internal/goals/`, queries in `backend/internal/db/queries/goals.sql`
  - [x] CRUD for savings/payoff/purchase goals: target amount, target date, linked category, optional linked account
  - [x] contributions: manual entry or linked to a transaction; current balance derived from contributions
  - [x] required-monthly-contribution and behind-schedule status via budgetmath
  - [x] write tests: contribution accounting, projection math, behind-schedule detection at boundaries (past date, met target)
  - [x] run project test suite - must pass before task 11

### Task 11: In-app notifications engine and API
**Files:**
  - Create: `backend/internal/notifications/`, queries in `backend/internal/db/queries/notifications.sql`
  - [x] notification storage + endpoints: list, unread count, mark read/all read
  - [x] per-user preferences: enable/disable per type, configurable warning threshold
  - [x] event-driven triggers: category reaches threshold, category over budget (evaluated after transaction/allocation writes)
  - [x] scheduled evaluator (on-request or ticker): bill due soon, budget month not created, goal behind schedule, import needs review
  - [x] dedupe so the same condition doesn't re-notify every write (one active notification per condition per period)
  - [x] write tests: threshold crossing fires exactly once, preference suppression, bill-due timing, dedupe behavior
  - [x] run project test suite - must pass before task 12

### Task 12: CSV import (mapping, preview, commit, batch undo)
**Files:**
  - Create: `backend/internal/importer/`, queries in `backend/internal/db/queries/imports.sql`
  - [x] upload endpoint: parse CSV (delimiter/encoding tolerant), return detected columns and sample rows
  - [x] column-mapping submission: map date/amount/payee/notes/category columns, date and amount format options, target account
  - [x] preview endpoint: validated rows with per-row errors, duplicate detection against existing transactions and within the file
  - [x] commit: create import batch + transactions atomically; skip or include flagged duplicates per user choice
  - [x] delete an entire import batch (removes its transactions, records audit event)
  - [x] write tests: mapping variants, malformed rows reported not crashing, duplicate flags, batch delete restores prior balances
  - [x] run project test suite - must pass before task 13

### Task 13: CSV export, full data export, and account deletion
**Files:**
  - Create: `backend/internal/exporter/`, deletion logic in `backend/internal/auth/` or `backend/internal/users/`
  - [x] CSV export endpoints: transactions (respecting current filters), monthly budget, categories, goals summary
  - [x] full data export: single ZIP of CSVs for every entity owned by the user
  - [x] account deletion: re-authentication required, deletes all user data in one transaction, records final audit event
  - [x] export requests logged as audit events
  - [x] write tests: export round-trips (export → parse → matches DB), deletion removes all rows across tables, deletion requires re-auth
  - [x] run project test suite - must pass before task 14

### Task 14: Dashboard and reports API
**Files:**
  - Create: `backend/internal/reports/`, queries in `backend/internal/db/queries/reports.sql`
  - [x] dashboard endpoint: total available balance (included accounts), MTD income/spending, remaining budget, budget health indicator, categories near/over limit, upcoming bills, recent transactions, goal progress
  - [x] report endpoints: spending by category, monthly spending trend, income vs expenses, cash-flow timeline, net worth across included accounts, top payees
  - [x] all reports scoped to the budget currency; foreign-currency accounts listed as informational balances, never converted
  - [x] write tests: aggregates against a seeded fixture set (transfers and refunds handled correctly), account-inclusion flag respected
  - [x] run project test suite - must pass before task 15

### Task 15: Frontend foundation — auth, app shell, API client
**Files:**
  - Create: `frontend/src/api/` (typed client, TanStack Query hooks), `frontend/src/routes/`, `frontend/src/components/layout/` (sidebar, mobile nav), auth pages
  - [x] typed API client with CSRF token handling, error normalization, and auth-expiry redirect
  - [x] sign-up, sign-in, password-reset, profile/settings pages with form validation (react-hook-form + zod)
  - [x] app shell: persistent desktop sidebar (Dashboard, Budget, Transactions, Goals, Reports), responsive mobile navigation with always-visible quick-add button, protected routes
  - [x] empty states for first-time use on each primary area
  - [x] write tests: auth form validation, protected-route redirect, shell renders nav on desktop and mobile viewports
  - [x] run project test suite - must pass before task 16

### Task 16: Frontend — accounts, categories, and settings screens
**Files:**
  - Create: `frontend/src/features/accounts/`, `frontend/src/features/categories/`, `frontend/src/features/settings/`
  - [x] account list + detail: create/edit/archive, opening balance, reconciliation flow, net-worth inclusion toggle
  - [x] category manager: groups and categories with create/rename/reorder/archive/merge, icon and color pickers, budget type and rollover rule selectors, default-set seeding
  - [x] settings screens: profile, notification preferences with threshold configuration, security (session list, sign-out-all), data (export all, delete account with confirmation)
  - [x] write tests: account creation flow, category reorder interaction, delete-account confirmation gating
  - [x] run project test suite - must pass before task 17

### Task 17: Frontend — budget workspace
**Files:**
  - Create: `frontend/src/features/budget/`
  - [x] monthly view with month switcher; create month (empty or copy prior)
  - [x] header: total income, "Available to assign" with explicit labeling, budget notes
  - [x] category table grouped by category group: budgeted, activity, remaining, status badge (color + icon + text, never color alone); inline editing of allocations
  - [x] move-money dialog between categories; suggested actions on over-budget rows
  - [x] responsive: table collapses to stacked cards on mobile
  - [x] write tests: allocation editing updates unallocated display, status badges by state, move-money flow
  - [x] run project test suite - must pass before task 18

### Task 18: Frontend — transactions list and editor
**Files:**
  - Create: `frontend/src/features/transactions/`
  - [ ] transaction list with pagination, filters (date, account, category, payee, amount, status, type, tags), and search
  - [ ] add/edit modal optimized for speed: date (defaults today), amount, account, payee, category; keyboard-first flow; expense/income/transfer/refund type switch
  - [ ] split editor with running remainder; transfer form with from/to accounts
  - [ ] bulk selection toolbar: categorize, tag, delete, mark reviewed; duplicate warnings surfaced on entry
  - [ ] soft-deleted view with restore
  - [ ] write tests: quick-add happy path, split sum validation, filter application, bulk categorize, restore
  - [ ] run project test suite - must pass before task 19

### Task 19: Frontend — recurring bills, goals, and notifications
**Files:**
  - Create: `frontend/src/features/recurring/`, `frontend/src/features/goals/`, `frontend/src/features/notifications/`
  - [ ] recurring page: rule list with next due dates, create/edit form, mark-as-paid and match-to-transaction actions
  - [ ] goals page: goal cards with progress bars, target/date form, contribution recording, required-monthly-contribution and behind-schedule indicator
  - [ ] notification center: bell with unread badge, list panel, mark read, links to the relevant screen; each warning includes a suggested action per PRD copy guidelines
  - [ ] write tests: recurring creation with next-due display, goal progress rendering, notification read state
  - [ ] run project test suite - must pass before task 20

### Task 20: Frontend — dashboard, reports, and import/export wizard
**Files:**
  - Create: `frontend/src/features/dashboard/`, `frontend/src/features/reports/`, `frontend/src/features/import/`
  - [ ] dashboard: balance/MTD/remaining cards, health indicator, near-limit categories, upcoming bills, recent transactions, goal progress, quick actions (add transaction, add income, transfer, create category, allocate funds)
  - [ ] reports: spending by category (chart + accessible table equivalent), monthly trend, income vs expenses, cash flow, net worth, top payees; CSV download buttons
  - [ ] import wizard: upload → column mapping → preview with errors and duplicate flags → commit; import-batch list with batch delete
  - [ ] write tests: dashboard renders from fixture API responses, import wizard step flow with validation errors, report CSV download trigger
  - [ ] run project test suite - must pass before task 21

### Task 21: Accessibility and responsive hardening
**Files:**
  - Modify: shared components and feature screens as flagged
  - [ ] keyboard navigation audit of essential flows (auth, quick-add transaction, budget editing, import wizard); fix focus traps and add visible focus states
  - [ ] ARIA labels for icon buttons, charts (with table fallbacks), and form controls; status conveyed by text + icon, not color alone
  - [ ] verify mobile layouts: tables to cards, quick-add reachable, charts readable
  - [ ] add axe-core automated a11y checks to component tests for key screens
  - [ ] run project test suite - must pass before task 22

### Task 22: Verify acceptance criteria
  - [ ] add an end-to-end backend integration test walking the 15 MVP acceptance criteria in sequence (sign-up through data export/deletion) against a real database
  - [ ] run full test suite (`make test`) - must pass
  - [ ] run linters (`golangci-lint`, `eslint`) - must pass
  - [ ] verify test coverage meets 80%+ on backend internal packages and frontend features

### Task 23: Update documentation
  - [ ] write README.md: project overview, architecture, local setup (docker-compose, migrations, dev servers), test/lint commands
  - [ ] create CLAUDE.md: repo conventions (money as minor units, sqlc workflow, migration numbering, feature-folder layout, test database usage)

## Post-Completion (manual, not automated)
  - Configure production SMTP for password-reset email delivery
  - Legal/compliance review before public launch (privacy policy, consent language)
  - Production deployment, TLS termination, and database backup configuration
