// Package exporter implements CSV export endpoints (transactions with list
// filters, monthly budget, categories, goals summary) and the full data
// export: one ZIP with a CSV per entity the user owns. Every export request
// is recorded as an audit event.
//
// Amounts are exported as raw minor units (the storage representation) so a
// round-trip through export → import loses nothing to formatting.
package exporter

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/audit"
	"budgetflow/internal/budgets"
	"budgetflow/internal/db"
	"budgetflow/internal/goals"
	"budgetflow/internal/transactions"
)

// EventDataExported is the audit event type recorded once per export request;
// the payload names which export was produced.
const EventDataExported = "data_exported"

// Service builds CSV and ZIP exports over the feature services (so exports
// share their math and filters) and the generated queries (for raw entities).
type Service struct {
	q       *db.Queries
	tx      *transactions.Service
	budgets *budgets.Service
	goals   *goals.Service
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		q:       db.New(pool),
		tx:      transactions.NewService(pool),
		budgets: budgets.NewService(pool),
		goals:   goals.NewService(db.New(pool)),
	}
}

func (s *Service) recordExport(ctx context.Context, userID uuid.UUID, kind string) error {
	return audit.Record(ctx, s.q, &userID, EventDataExported, "export", nil, map[string]string{"export": kind})
}

// nameMaps loads id→name lookups so exported rows carry human-readable
// account and category names alongside amounts.
func (s *Service) nameMaps(ctx context.Context, userID uuid.UUID) (accounts, categories map[uuid.UUID]string, err error) {
	accs, err := s.q.ListAccountsByUser(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("list accounts: %w", err)
	}
	cats, err := s.q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("list categories: %w", err)
	}
	accounts = make(map[uuid.UUID]string, len(accs))
	for _, a := range accs {
		accounts[a.ID] = a.Name
	}
	categories = make(map[uuid.UUID]string, len(cats))
	for _, c := range cats {
		categories[c.ID] = c.Name
	}
	return accounts, categories, nil
}

// TransactionsCSV exports every transaction matching the filter (the same
// filter set as the list endpoint), paging internally so the export is
// complete regardless of the list page size.
func (s *Service) TransactionsCSV(ctx context.Context, userID uuid.UUID, f transactions.Filter) ([]byte, error) {
	accNames, catNames, err := s.nameMaps(ctx, userID)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"id", "date", "type", "status", "amount_minor", "account", "payee", "category", "splits", "tags", "notes", "reviewed"}); err != nil {
		return nil, fmt.Errorf("write csv header: %w", err)
	}

	f.Limit = 200
	f.Offset = 0
	for {
		page, err := s.tx.List(ctx, userID, f)
		if err != nil {
			return nil, err
		}
		for _, d := range page.Transactions {
			t := d.Transaction
			category := ""
			if t.CategoryID != nil {
				category = catNames[*t.CategoryID]
			}
			splits := make([]string, 0, len(d.Splits))
			for _, sp := range d.Splits {
				splits = append(splits, fmt.Sprintf("%s=%d", catNames[sp.CategoryID], sp.Amount))
			}
			tags := make([]string, 0, len(d.Tags))
			for _, tg := range d.Tags {
				tags = append(tags, tg.Name)
			}
			row := []string{
				t.ID.String(), t.Date.Format("2006-01-02"), t.Type, t.Status,
				strconv.FormatInt(t.Amount, 10), accNames[t.AccountID], t.Payee,
				category, strings.Join(splits, ";"), strings.Join(tags, ";"),
				t.Notes, strconv.FormatBool(t.Reviewed),
			}
			if err := w.Write(row); err != nil {
				return nil, fmt.Errorf("write csv row: %w", err)
			}
		}
		f.Offset += f.Limit
		if int64(f.Offset) >= page.Total || len(page.Transactions) == 0 {
			break
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}
	if err := s.recordExport(ctx, userID, "transactions"); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BudgetCSV exports one monthly budget: a row per allocated category with the
// computed activity/remaining/status, with period-level figures repeated on
// every row so each line is self-describing.
func (s *Service) BudgetCSV(ctx context.Context, userID uuid.UUID, year, month int) ([]byte, error) {
	detail, err := s.budgets.Get(ctx, userID, year, month)
	if err != nil {
		return nil, err
	}
	_, catNames, err := s.nameMaps(ctx, userID)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"year", "month", "currency", "planned_income_minor", "unallocated_minor", "category_id", "category", "budgeted_minor", "rollover_minor", "activity_minor", "reserved_minor", "remaining_minor", "status"}); err != nil {
		return nil, fmt.Errorf("write csv header: %w", err)
	}
	p := detail.Period
	for _, c := range detail.Categories {
		row := []string{
			strconv.Itoa(int(p.Year)), strconv.Itoa(int(p.Month)), p.Currency,
			strconv.FormatInt(p.PlannedIncome, 10), strconv.FormatInt(detail.Unallocated, 10),
			c.CategoryID.String(), catNames[c.CategoryID],
			strconv.FormatInt(c.Amount, 10), strconv.FormatInt(c.Rollover, 10),
			strconv.FormatInt(c.Spending, 10), strconv.FormatInt(c.Reserved, 10),
			strconv.FormatInt(c.Remaining, 10), string(c.Status),
		}
		if err := w.Write(row); err != nil {
			return nil, fmt.Errorf("write csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}
	if err := s.recordExport(ctx, userID, "budget"); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CategoriesCSV exports every category (including archived) with its group.
func (s *Service) CategoriesCSV(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	groups, err := s.q.ListCategoryGroupsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list category groups: %w", err)
	}
	cats, err := s.q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	groupNames := make(map[uuid.UUID]string, len(groups))
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"id", "group", "name", "budget_type", "rollover_rule", "icon", "color", "sort_order", "archived"}); err != nil {
		return nil, fmt.Errorf("write csv header: %w", err)
	}
	for _, c := range cats {
		row := []string{
			c.ID.String(), groupNames[c.GroupID], c.Name, c.BudgetType, c.RolloverRule,
			c.Icon, c.Color, strconv.Itoa(int(c.SortOrder)), strconv.FormatBool(c.ArchivedAt != nil),
		}
		if err := w.Write(row); err != nil {
			return nil, fmt.Errorf("write csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}
	if err := s.recordExport(ctx, userID, "categories"); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GoalsCSV exports the active goals summary with derived progress figures.
func (s *Service) GoalsCSV(ctx context.Context, userID uuid.UUID, now time.Time) ([]byte, error) {
	details, err := s.goals.List(ctx, userID, now)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"id", "name", "type", "target_amount_minor", "target_date", "current_balance_minor", "amount_remaining_minor", "required_monthly_minor", "behind_schedule"}); err != nil {
		return nil, fmt.Errorf("write csv header: %w", err)
	}
	for _, d := range details {
		targetDate := ""
		if d.Goal.TargetDate != nil {
			targetDate = d.Goal.TargetDate.Format("2006-01-02")
		}
		requiredMonthly := ""
		if d.Progress.RequiredMonthly != nil {
			requiredMonthly = strconv.FormatInt(*d.Progress.RequiredMonthly, 10)
		}
		row := []string{
			d.Goal.ID.String(), d.Goal.Name, d.Goal.Type,
			strconv.FormatInt(d.Goal.TargetAmount, 10), targetDate,
			strconv.FormatInt(d.Progress.CurrentBalance, 10),
			strconv.FormatInt(d.Progress.AmountRemaining, 10),
			requiredMonthly, strconv.FormatBool(d.Progress.BehindSchedule),
		}
		if err := w.Write(row); err != nil {
			return nil, fmt.Errorf("write csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}
	if err := s.recordExport(ctx, userID, "goals"); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
