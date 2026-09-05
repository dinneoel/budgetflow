package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// FullExportZIP builds one ZIP archive containing a CSV per entity the user
// owns, including archived and soft-deleted rows. Rows are the raw storage
// representation (ids, minor units) so the archive is a faithful copy of the
// user's data.
func (s *Service) FullExportZIP(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if err := writeCSV(zw, "profile.csv",
		[]string{"id", "email", "name", "locale", "time_zone", "first_day_of_week", "default_currency", "created_at"},
		[][]string{{user.ID.String(), user.Email, user.Name, user.Locale, user.TimeZone,
			strconv.Itoa(int(user.FirstDayOfWeek)), user.DefaultCurrency, ts(user.CreatedAt)}}); err != nil {
		return nil, err
	}

	if err := s.writeAccounts(ctx, zw, userID); err != nil {
		return nil, err
	}
	if err := s.writeCategories(ctx, zw, userID); err != nil {
		return nil, err
	}
	if err := s.writeBudgets(ctx, zw, userID); err != nil {
		return nil, err
	}
	if err := s.writeTransactions(ctx, zw, userID); err != nil {
		return nil, err
	}
	if err := s.writeRecurring(ctx, zw, userID); err != nil {
		return nil, err
	}
	if err := s.writeGoals(ctx, zw, userID); err != nil {
		return nil, err
	}
	if err := s.writeImportsAndNotifications(ctx, zw, userID); err != nil {
		return nil, err
	}
	if err := s.writeAuditEvents(ctx, zw, userID); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	if err := s.recordExport(ctx, userID, "full"); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Service) writeAccounts(ctx context.Context, zw *zip.Writer, userID uuid.UUID) error {
	accs, err := s.q.ListAccountsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	rows := make([][]string, 0, len(accs))
	for _, a := range accs {
		rows = append(rows, []string{a.ID.String(), a.Name, a.Institution, a.Type, a.Currency,
			strconv.FormatInt(a.OpeningBalance, 10), strconv.FormatBool(a.IncludeInNetWorth),
			optTS(a.ArchivedAt), ts(a.CreatedAt)})
	}
	return writeCSV(zw, "accounts.csv",
		[]string{"id", "name", "institution", "type", "currency", "opening_balance_minor", "include_in_net_worth", "archived_at", "created_at"}, rows)
}

func (s *Service) writeCategories(ctx context.Context, zw *zip.Writer, userID uuid.UUID) error {
	groups, err := s.q.ListCategoryGroupsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list category groups: %w", err)
	}
	rows := make([][]string, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, []string{g.ID.String(), g.Name, strconv.Itoa(int(g.SortOrder)), optTS(g.ArchivedAt), ts(g.CreatedAt)})
	}
	if err := writeCSV(zw, "category_groups.csv", []string{"id", "name", "sort_order", "archived_at", "created_at"}, rows); err != nil {
		return err
	}

	cats, err := s.q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list categories: %w", err)
	}
	rows = rows[:0]
	for _, c := range cats {
		rows = append(rows, []string{c.ID.String(), c.GroupID.String(), c.Name, c.Icon, c.Color,
			c.BudgetType, c.RolloverRule, strconv.Itoa(int(c.SortOrder)), optTS(c.ArchivedAt), ts(c.CreatedAt)})
	}
	return writeCSV(zw, "categories.csv",
		[]string{"id", "group_id", "name", "icon", "color", "budget_type", "rollover_rule", "sort_order", "archived_at", "created_at"}, rows)
}

func (s *Service) writeBudgets(ctx context.Context, zw *zip.Writer, userID uuid.UUID) error {
	periods, err := s.q.ListBudgetPeriodsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list budget periods: %w", err)
	}
	rows := make([][]string, 0, len(periods))
	for _, p := range periods {
		rows = append(rows, []string{p.ID.String(), strconv.Itoa(int(p.Year)), strconv.Itoa(int(p.Month)),
			p.Currency, strconv.FormatInt(p.PlannedIncome, 10), p.Notes, ts(p.CreatedAt)})
	}
	if err := writeCSV(zw, "budget_periods.csv",
		[]string{"id", "year", "month", "currency", "planned_income_minor", "notes", "created_at"}, rows); err != nil {
		return err
	}

	allocs, err := s.q.ExportAllocationsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list allocations: %w", err)
	}
	rows = rows[:0]
	for _, a := range allocs {
		rows = append(rows, []string{a.ID.String(), a.PeriodID.String(), strconv.Itoa(int(a.Year)),
			strconv.Itoa(int(a.Month)), a.CategoryID.String(),
			strconv.FormatInt(a.Amount, 10), strconv.FormatInt(a.Rollover, 10)})
	}
	return writeCSV(zw, "budget_allocations.csv",
		[]string{"id", "period_id", "year", "month", "category_id", "amount_minor", "rollover_minor"}, rows)
}

func (s *Service) writeTransactions(ctx context.Context, zw *zip.Writer, userID uuid.UUID) error {
	txs, err := s.q.ExportTransactionsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list transactions: %w", err)
	}
	rows := make([][]string, 0, len(txs))
	for _, t := range txs {
		rows = append(rows, []string{t.ID.String(), t.AccountID.String(), optID(t.CategoryID),
			t.Type, t.Status, strconv.FormatInt(t.Amount, 10), t.Date.Format("2006-01-02"),
			t.Payee, t.Notes, strconv.FormatBool(t.Reviewed), optID(t.TransferPairID),
			optID(t.RecurringRuleID), optID(t.ImportBatchID), optTS(t.DeletedAt), ts(t.CreatedAt)})
	}
	if err := writeCSV(zw, "transactions.csv",
		[]string{"id", "account_id", "category_id", "type", "status", "amount_minor", "date", "payee", "notes",
			"reviewed", "transfer_pair_id", "recurring_rule_id", "import_batch_id", "deleted_at", "created_at"}, rows); err != nil {
		return err
	}

	splits, err := s.q.ExportSplitsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list splits: %w", err)
	}
	rows = rows[:0]
	for _, sp := range splits {
		rows = append(rows, []string{sp.ID.String(), sp.TransactionID.String(), sp.CategoryID.String(),
			strconv.FormatInt(sp.Amount, 10), sp.Memo})
	}
	if err := writeCSV(zw, "transaction_splits.csv",
		[]string{"id", "transaction_id", "category_id", "amount_minor", "memo"}, rows); err != nil {
		return err
	}

	tags, err := s.q.ListTagsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list tags: %w", err)
	}
	rows = rows[:0]
	for _, tg := range tags {
		rows = append(rows, []string{tg.ID.String(), tg.Name, ts(tg.CreatedAt)})
	}
	if err := writeCSV(zw, "tags.csv", []string{"id", "name", "created_at"}, rows); err != nil {
		return err
	}

	txTags, err := s.q.ExportTransactionTagsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list transaction tags: %w", err)
	}
	rows = rows[:0]
	for _, tt := range txTags {
		rows = append(rows, []string{tt.TransactionID.String(), tt.Name})
	}
	return writeCSV(zw, "transaction_tags.csv", []string{"transaction_id", "tag"}, rows)
}

func (s *Service) writeRecurring(ctx context.Context, zw *zip.Writer, userID uuid.UUID) error {
	rules, err := s.q.ExportRecurringRulesByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list recurring rules: %w", err)
	}
	rows := make([][]string, 0, len(rules))
	for _, r := range rules {
		interval := ""
		if r.CustomIntervalDays != nil {
			interval = strconv.Itoa(int(*r.CustomIntervalDays))
		}
		rows = append(rows, []string{r.ID.String(), r.Name, r.AccountID.String(), r.CategoryID.String(),
			strconv.FormatInt(r.Amount, 10), r.Frequency, interval, r.NextDueDate.Format("2006-01-02"),
			strconv.Itoa(int(r.ReminderLeadDays)), optTS(r.ArchivedAt), ts(r.CreatedAt)})
	}
	return writeCSV(zw, "recurring_rules.csv",
		[]string{"id", "name", "account_id", "category_id", "amount_minor", "frequency", "custom_interval_days",
			"next_due_date", "reminder_lead_days", "archived_at", "created_at"}, rows)
}

func (s *Service) writeGoals(ctx context.Context, zw *zip.Writer, userID uuid.UUID) error {
	gs, err := s.q.ExportGoalsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list goals: %w", err)
	}
	rows := make([][]string, 0, len(gs))
	for _, g := range gs {
		targetDate := ""
		if g.TargetDate != nil {
			targetDate = g.TargetDate.Format("2006-01-02")
		}
		rows = append(rows, []string{g.ID.String(), g.Name, g.Type, strconv.FormatInt(g.TargetAmount, 10),
			targetDate, optID(g.CategoryID), optID(g.AccountID), optTS(g.ArchivedAt), ts(g.CreatedAt)})
	}
	if err := writeCSV(zw, "goals.csv",
		[]string{"id", "name", "type", "target_amount_minor", "target_date", "category_id", "account_id", "archived_at", "created_at"}, rows); err != nil {
		return err
	}

	contribs, err := s.q.ExportGoalContributionsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list goal contributions: %w", err)
	}
	rows = rows[:0]
	for _, c := range contribs {
		rows = append(rows, []string{c.ID.String(), c.GoalID.String(), optID(c.TransactionID),
			strconv.FormatInt(c.Amount, 10), c.ContributedOn.Format("2006-01-02"), c.Notes})
	}
	return writeCSV(zw, "goal_contributions.csv",
		[]string{"id", "goal_id", "transaction_id", "amount_minor", "contributed_on", "notes"}, rows)
}

func (s *Service) writeImportsAndNotifications(ctx context.Context, zw *zip.Writer, userID uuid.UUID) error {
	batches, err := s.q.ListImportBatchesByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list import batches: %w", err)
	}
	rows := make([][]string, 0, len(batches))
	for _, b := range batches {
		rows = append(rows, []string{b.ID.String(), optID(b.AccountID), b.FileName, b.Status,
			strconv.Itoa(int(b.RowCount)), optTS(b.CommittedAt), ts(b.CreatedAt)})
	}
	if err := writeCSV(zw, "import_batches.csv",
		[]string{"id", "account_id", "file_name", "status", "row_count", "committed_at", "created_at"}, rows); err != nil {
		return err
	}

	notifs, err := s.q.ExportNotificationsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list notifications: %w", err)
	}
	rows = rows[:0]
	for _, n := range notifs {
		rows = append(rows, []string{n.ID.String(), n.Type, n.Title, n.Body, n.ActionUrl, optTS(n.ReadAt), ts(n.CreatedAt)})
	}
	return writeCSV(zw, "notifications.csv",
		[]string{"id", "type", "title", "body", "action_url", "read_at", "created_at"}, rows)
}

func (s *Service) writeAuditEvents(ctx context.Context, zw *zip.Writer, userID uuid.UUID) error {
	events, err := s.q.ExportAuditEventsByUser(ctx, &userID)
	if err != nil {
		return fmt.Errorf("list audit events: %w", err)
	}
	rows := make([][]string, 0, len(events))
	for _, e := range events {
		ids := make([]string, 0, len(e.EntityIds))
		for _, id := range e.EntityIds {
			ids = append(ids, id.String())
		}
		rows = append(rows, []string{e.ID.String(), e.EventType, e.EntityType,
			strings.Join(ids, ";"), string(e.Payload), ts(e.CreatedAt)})
	}
	return writeCSV(zw, "audit_events.csv",
		[]string{"id", "event_type", "entity_type", "entity_ids", "payload", "created_at"}, rows)
}

func writeCSV(zw *zip.Writer, name string, header []string, rows [][]string) error {
	f, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create %s in zip: %w", name, err)
	}
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write %s header: %w", name, err)
	}
	if err := w.WriteAll(rows); err != nil {
		return fmt.Errorf("write %s rows: %w", name, err)
	}
	w.Flush()
	return w.Error()
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func optTS(t *time.Time) string {
	if t == nil {
		return ""
	}
	return ts(*t)
}

func optID(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
