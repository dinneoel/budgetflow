-- BudgetFlow core schema.
--
-- Conventions:
--   * All monetary amounts are BIGINT minor units (cents/kopiykas) — never floats.
--   * Every user-owned table carries user_id and a UNIQUE (id, user_id) key so that
--     child tables can reference parents with composite FKs, making cross-user
--     references impossible at the schema level.
--   * Soft delete via deleted_at (transactions) and archived_at (accounts,
--     categories, groups, recurring rules, goals).

BEGIN;

CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email           text NOT NULL,
    password_hash   text NOT NULL,
    name            text NOT NULL DEFAULT '',
    locale          text NOT NULL DEFAULT 'en-US',
    time_zone       text NOT NULL DEFAULT 'UTC',
    first_day_of_week smallint NOT NULL DEFAULT 1 CHECK (first_day_of_week BETWEEN 0 AND 6),
    default_currency text NOT NULL DEFAULT 'USD' CHECK (char_length(default_currency) = 3),
    failed_login_attempts int NOT NULL DEFAULT 0,
    locked_until    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE sessions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  text NOT NULL UNIQUE,
    user_agent  text NOT NULL DEFAULT '',
    ip_address  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE password_reset_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  text NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens (user_id);

CREATE TABLE accounts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (name <> ''),
    institution     text NOT NULL DEFAULT '',
    type            text NOT NULL CHECK (type IN ('cash', 'checking', 'savings', 'credit_card', 'ewallet', 'custom')),
    currency        text NOT NULL CHECK (char_length(currency) = 3),
    opening_balance bigint NOT NULL DEFAULT 0,
    include_in_net_worth boolean NOT NULL DEFAULT true,
    archived_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id)
);

CREATE INDEX accounts_user_id_idx ON accounts (user_id);

CREATE TABLE category_groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (name <> ''),
    sort_order  int NOT NULL DEFAULT 0,
    archived_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id)
);

CREATE INDEX category_groups_user_id_idx ON category_groups (user_id);

CREATE TABLE categories (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    group_id      uuid NOT NULL,
    name          text NOT NULL CHECK (name <> ''),
    icon          text NOT NULL DEFAULT '',
    color         text NOT NULL DEFAULT '',
    budget_type   text NOT NULL DEFAULT 'variable' CHECK (budget_type IN ('fixed', 'variable', 'sinking_fund', 'debt', 'savings_goal')),
    rollover_rule text NOT NULL DEFAULT 'none' CHECK (rollover_rule IN ('none', 'rollover', 'reset_to_target')),
    sort_order    int NOT NULL DEFAULT 0,
    archived_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    FOREIGN KEY (group_id, user_id) REFERENCES category_groups (id, user_id) ON DELETE CASCADE
);

CREATE INDEX categories_user_id_idx ON categories (user_id);
CREATE INDEX categories_group_id_idx ON categories (group_id);

CREATE TABLE budget_periods (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    year           int NOT NULL CHECK (year BETWEEN 2000 AND 2200),
    month          int NOT NULL CHECK (month BETWEEN 1 AND 12),
    currency       text NOT NULL CHECK (char_length(currency) = 3),
    planned_income bigint NOT NULL DEFAULT 0,
    notes          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    UNIQUE (user_id, year, month)
);

CREATE TABLE budget_allocations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    period_id   uuid NOT NULL,
    category_id uuid NOT NULL,
    amount      bigint NOT NULL DEFAULT 0,
    rollover    bigint NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    UNIQUE (period_id, category_id),
    FOREIGN KEY (period_id, user_id) REFERENCES budget_periods (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE
);

CREATE INDEX budget_allocations_period_id_idx ON budget_allocations (period_id);
CREATE INDEX budget_allocations_category_id_idx ON budget_allocations (category_id);

-- Append-only log of allocation and planned-income changes within a period.
-- category_id is NULL for period-level changes (planned income).
CREATE TABLE allocation_history (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    period_id   uuid NOT NULL,
    category_id uuid,
    field       text NOT NULL DEFAULT 'allocation' CHECK (field IN ('allocation', 'planned_income')),
    old_amount  bigint NOT NULL,
    new_amount  bigint NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (period_id, user_id) REFERENCES budget_periods (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE,
    CHECK (field = 'planned_income' OR category_id IS NOT NULL)
);

CREATE INDEX allocation_history_period_id_idx ON allocation_history (period_id);

CREATE TABLE import_batches (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id   uuid NOT NULL,
    file_name    text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'committed', 'deleted')),
    row_count    int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    committed_at timestamptz,
    UNIQUE (id, user_id),
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE
);

CREATE INDEX import_batches_user_id_idx ON import_batches (user_id);

CREATE TABLE recurring_rules (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                 text NOT NULL CHECK (name <> ''),
    account_id           uuid NOT NULL,
    category_id          uuid NOT NULL,
    amount               bigint NOT NULL,
    frequency            text NOT NULL CHECK (frequency IN ('weekly', 'monthly', 'annual', 'custom')),
    custom_interval_days int CHECK (custom_interval_days > 0),
    next_due_date        date NOT NULL,
    reminder_lead_days   int NOT NULL DEFAULT 3 CHECK (reminder_lead_days >= 0),
    archived_at          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE,
    CHECK (frequency <> 'custom' OR custom_interval_days IS NOT NULL)
);

CREATE INDEX recurring_rules_user_id_idx ON recurring_rules (user_id);
CREATE INDEX recurring_rules_next_due_date_idx ON recurring_rules (next_due_date);

CREATE TABLE transactions (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    account_id        uuid NOT NULL,
    category_id       uuid,
    type              text NOT NULL CHECK (type IN ('income', 'expense', 'refund', 'adjustment', 'transfer')),
    status            text NOT NULL DEFAULT 'uncleared' CHECK (status IN ('uncleared', 'cleared', 'reconciled')),
    amount            bigint NOT NULL,
    date              date NOT NULL,
    payee             text NOT NULL DEFAULT '',
    notes             text NOT NULL DEFAULT '',
    reviewed          boolean NOT NULL DEFAULT false,
    transfer_pair_id  uuid REFERENCES transactions (id) ON DELETE SET NULL,
    recurring_rule_id uuid,
    import_batch_id   uuid,
    deleted_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE SET NULL,
    FOREIGN KEY (recurring_rule_id, user_id) REFERENCES recurring_rules (id, user_id) ON DELETE SET NULL,
    FOREIGN KEY (import_batch_id, user_id) REFERENCES import_batches (id, user_id) ON DELETE SET NULL,
    -- transfers move money between accounts and are never categorized
    CHECK (type <> 'transfer' OR category_id IS NULL)
);

CREATE INDEX transactions_user_date_idx ON transactions (user_id, date DESC) WHERE deleted_at IS NULL;
CREATE INDEX transactions_account_id_idx ON transactions (account_id);
CREATE INDEX transactions_category_id_idx ON transactions (category_id);
CREATE INDEX transactions_import_batch_id_idx ON transactions (import_batch_id);

CREATE TABLE transaction_splits (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    transaction_id uuid NOT NULL,
    category_id    uuid NOT NULL,
    amount         bigint NOT NULL,
    memo           text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (transaction_id, user_id) REFERENCES transactions (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE
);

CREATE INDEX transaction_splits_transaction_id_idx ON transaction_splits (transaction_id);
CREATE INDEX transaction_splits_category_id_idx ON transaction_splits (category_id);

CREATE TABLE tags (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (name <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    UNIQUE (user_id, name)
);

CREATE TABLE transaction_tags (
    transaction_id uuid NOT NULL,
    tag_id         uuid NOT NULL,
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (transaction_id, tag_id),
    FOREIGN KEY (transaction_id, user_id) REFERENCES transactions (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id, user_id) REFERENCES tags (id, user_id) ON DELETE CASCADE
);

CREATE INDEX transaction_tags_tag_id_idx ON transaction_tags (tag_id);

CREATE TABLE goals (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name          text NOT NULL CHECK (name <> ''),
    type          text NOT NULL CHECK (type IN ('savings', 'payoff', 'purchase')),
    target_amount bigint NOT NULL CHECK (target_amount > 0),
    target_date   date,
    category_id   uuid,
    account_id    uuid,
    archived_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE SET NULL,
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE SET NULL
);

CREATE INDEX goals_user_id_idx ON goals (user_id);

CREATE TABLE goal_contributions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    goal_id        uuid NOT NULL,
    transaction_id uuid,
    amount         bigint NOT NULL,
    contributed_on date NOT NULL,
    notes          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (goal_id, user_id) REFERENCES goals (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (transaction_id, user_id) REFERENCES transactions (id, user_id) ON DELETE SET NULL
);

CREATE INDEX goal_contributions_goal_id_idx ON goal_contributions (goal_id);

CREATE TABLE notifications (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type       text NOT NULL CHECK (type IN ('category_threshold', 'category_over_budget', 'bill_due', 'budget_month_missing', 'goal_behind_schedule', 'import_needs_review')),
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    action_url text NOT NULL DEFAULT '',
    -- one active (unread) notification per condition; the engine builds the key
    -- from type + entity + period so the same condition never re-notifies
    dedupe_key text NOT NULL,
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX notifications_active_dedupe_key ON notifications (user_id, dedupe_key) WHERE read_at IS NULL;
CREATE INDEX notifications_user_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

CREATE TABLE notification_preferences (
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type          text NOT NULL CHECK (type IN ('category_threshold', 'category_over_budget', 'bill_due', 'budget_month_missing', 'goal_behind_schedule', 'import_needs_review')),
    enabled       boolean NOT NULL DEFAULT true,
    threshold_pct smallint CHECK (threshold_pct BETWEEN 1 AND 100),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, type)
);

-- Audit trail. user_id is nullable so the final "account deleted" event
-- survives the cascade that removes everything else the user owned.
CREATE TABLE audit_events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid REFERENCES users (id) ON DELETE SET NULL,
    event_type  text NOT NULL,
    entity_type text NOT NULL DEFAULT '',
    entity_ids  uuid[] NOT NULL DEFAULT '{}',
    payload     jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_events_user_id_idx ON audit_events (user_id, created_at DESC);

COMMIT;
