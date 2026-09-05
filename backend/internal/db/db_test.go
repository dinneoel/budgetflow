package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"budgetflow/internal/db"
	"budgetflow/internal/testdb"
)

const (
	pgUniqueViolation = "23505"
	pgFKViolation     = "23503"
	pgCheckViolation  = "23514"
)

func pgErrCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
	}
	return pgErr.Code
}

func TestMigrationsRoundTrip(t *testing.T) {
	t.Parallel()
	dbURL := testdb.NewEmpty(t)

	m, err := testdb.NewMigrator(dbURL)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if err := m.Down(); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up after down: %v", err)
	}
}

// seedUser creates a user plus the minimal object graph most constraint tests
// need: one account, one category group, and one category.
func seedUser(t *testing.T, ctx context.Context, q *db.Queries, email string) (db.User, db.Account, db.Category) {
	t.Helper()
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		Email: email, PasswordHash: "x", Name: "Test", DefaultCurrency: "UAH",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	account, err := q.CreateAccount(ctx, db.CreateAccountParams{
		UserID: user.ID, Name: "Wallet", Type: "checking", Currency: "UAH", OpeningBalance: 10_000, IncludeInNetWorth: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	group, err := q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: user.ID, Name: "Essentials"})
	if err != nil {
		t.Fatalf("create category group: %v", err)
	}
	category, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: user.ID, GroupID: group.ID, Name: "Groceries", BudgetType: "variable", RolloverRule: "none",
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	return user, account, category
}

func TestDuplicateBudgetPeriodRejected(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	user, _, _ := seedUser(t, ctx, q, "period@example.com")

	params := db.CreateBudgetPeriodParams{UserID: user.ID, Year: 2026, Month: 9, Currency: "UAH"}
	if _, err := q.CreateBudgetPeriod(ctx, params); err != nil {
		t.Fatalf("create first period: %v", err)
	}
	_, err := q.CreateBudgetPeriod(ctx, params)
	if code := pgErrCode(t, err); code != pgUniqueViolation {
		t.Fatalf("duplicate period: want unique violation %s, got %s", pgUniqueViolation, code)
	}
}

func TestOrphanedSplitRejected(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	user, _, category := seedUser(t, ctx, q, "split@example.com")

	_, err := q.CreateTransactionSplit(ctx, db.CreateTransactionSplitParams{
		UserID: user.ID, TransactionID: uuid.New(), CategoryID: category.ID, Amount: -500,
	})
	if code := pgErrCode(t, err); code != pgFKViolation {
		t.Fatalf("orphaned split: want FK violation %s, got %s", pgFKViolation, code)
	}
}

func TestCrossUserReferenceRejected(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	_, _, categoryA := seedUser(t, ctx, q, "alice@example.com")
	userB, accountB, _ := seedUser(t, ctx, q, "bob@example.com")

	// Bob's transaction must not be able to point at Alice's category: the
	// composite (category_id, user_id) FK has no matching row.
	_, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: userB.ID, AccountID: accountB.ID, CategoryID: &categoryA.ID,
		Type: "expense", Status: "uncleared", Amount: -1000, Date: date(2026, 9, 5),
	})
	if code := pgErrCode(t, err); code != pgFKViolation {
		t.Fatalf("cross-user category: want FK violation %s, got %s", pgFKViolation, code)
	}
}

func TestTransactionCheckConstraints(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	user, account, category := seedUser(t, ctx, q, "checks@example.com")

	base := db.CreateTransactionParams{
		UserID: user.ID, AccountID: account.ID, CategoryID: &category.ID,
		Type: "expense", Status: "uncleared", Amount: -1000, Date: date(2026, 9, 5),
	}

	badType := base
	badType.Type = "withdrawal"
	if code := pgErrCode(t, mustErr(q.CreateTransaction(ctx, badType))); code != pgCheckViolation {
		t.Fatalf("bad type: want check violation %s, got %s", pgCheckViolation, code)
	}

	badStatus := base
	badStatus.Status = "posted"
	if code := pgErrCode(t, mustErr(q.CreateTransaction(ctx, badStatus))); code != pgCheckViolation {
		t.Fatalf("bad status: want check violation %s, got %s", pgCheckViolation, code)
	}

	categorizedTransfer := base
	categorizedTransfer.Type = "transfer"
	if code := pgErrCode(t, mustErr(q.CreateTransaction(ctx, categorizedTransfer))); code != pgCheckViolation {
		t.Fatalf("categorized transfer: want check violation %s, got %s", pgCheckViolation, code)
	}
}

func TestSoftDeleteAndBalance(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	user, account, category := seedUser(t, ctx, q, "balance@example.com")

	tx, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: user.ID, AccountID: account.ID, CategoryID: &category.ID,
		Type: "expense", Status: "uncleared", Amount: -2_500, Date: date(2026, 9, 1),
	})
	if err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	assertBalance := func(want int64) {
		t.Helper()
		got, err := q.GetAccountBalance(ctx, db.GetAccountBalanceParams{ID: account.ID, UserID: user.ID})
		if err != nil {
			t.Fatalf("get balance: %v", err)
		}
		if got != want {
			t.Fatalf("balance: want %d, got %d", want, got)
		}
	}

	assertBalance(7_500) // 10_000 opening − 2_500

	if err := q.SoftDeleteTransaction(ctx, db.SoftDeleteTransactionParams{ID: tx.ID, UserID: user.ID}); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	assertBalance(10_000) // deleted transactions do not count

	if err := q.RestoreTransaction(ctx, db.RestoreTransactionParams{ID: tx.ID, UserID: user.ID}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	assertBalance(7_500)
}

func TestAllocationUpsertAndHistory(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	user, _, category := seedUser(t, ctx, q, "alloc@example.com")

	period, err := q.CreateBudgetPeriod(ctx, db.CreateBudgetPeriodParams{
		UserID: user.ID, Year: 2026, Month: 9, Currency: "UAH", PlannedIncome: 100_000,
	})
	if err != nil {
		t.Fatalf("create period: %v", err)
	}

	alloc, err := q.UpsertBudgetAllocation(ctx, db.UpsertBudgetAllocationParams{
		UserID: user.ID, PeriodID: period.ID, CategoryID: category.ID, Amount: 30_000,
	})
	if err != nil {
		t.Fatalf("insert allocation: %v", err)
	}
	updated, err := q.UpsertBudgetAllocation(ctx, db.UpsertBudgetAllocationParams{
		UserID: user.ID, PeriodID: period.ID, CategoryID: category.ID, Amount: 45_000,
	})
	if err != nil {
		t.Fatalf("upsert allocation: %v", err)
	}
	if updated.ID != alloc.ID {
		t.Fatalf("upsert created a new row instead of updating: %s != %s", updated.ID, alloc.ID)
	}
	if updated.Amount != 45_000 {
		t.Fatalf("upsert amount: want 45000, got %d", updated.Amount)
	}

	if _, err := q.CreateAllocationHistory(ctx, db.CreateAllocationHistoryParams{
		UserID: user.ID, PeriodID: period.ID, CategoryID: &category.ID,
		Field: "allocation", OldAmount: 30_000, NewAmount: 45_000,
	}); err != nil {
		t.Fatalf("record history: %v", err)
	}
	history, err := q.ListAllocationHistoryByPeriod(ctx, db.ListAllocationHistoryByPeriodParams{PeriodID: period.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history rows: want 1, got %d", len(history))
	}
}

func TestUserCascadeDeleteAndAuditSurvives(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	user, account, category := seedUser(t, ctx, q, "cascade@example.com")

	if _, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: user.ID, AccountID: account.ID, CategoryID: &category.ID,
		Type: "expense", Status: "uncleared", Amount: -100, Date: date(2026, 9, 3),
	}); err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	audit, err := q.CreateAuditEvent(ctx, db.CreateAuditEventParams{
		UserID: &user.ID, EventType: "account.deleted", EntityType: "user",
		EntityIds: []uuid.UUID{user.ID}, Payload: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create audit event: %v", err)
	}

	if err := q.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	for _, table := range []string{"accounts", "categories", "category_groups", "transactions"} {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Fatalf("%s: want 0 rows after user delete, got %d", table, n)
		}
	}

	// the audit trail outlives the user, with user_id nulled
	var auditUser *uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT user_id FROM audit_events WHERE id = $1", audit.ID).Scan(&auditUser); err != nil {
		t.Fatalf("audit event vanished: %v", err)
	}
	if auditUser != nil {
		t.Fatalf("audit user_id: want NULL, got %s", auditUser)
	}
}

func mustErr[T any](_ T, err error) error { return err }

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
