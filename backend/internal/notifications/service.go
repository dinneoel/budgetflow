// Package notifications implements the in-app notification engine and API:
// storage endpoints (list, unread count, mark read), per-user preferences with
// a configurable warning threshold, event-driven category triggers evaluated
// after transaction/allocation writes, and a scheduled evaluator for bill-due,
// missing budget month, goal-behind-schedule, and import-review conditions.
//
// Dedupe: the notifications table has a partial unique index on
// (user_id, dedupe_key) WHERE read_at IS NULL, and the engine builds keys from
// type + entity + period, so a persisting condition keeps at most one active
// notification per period no matter how often it is re-evaluated.
package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"budgetflow/internal/budgetmath"
	"budgetflow/internal/db"
)

// Notification types, mirroring the notifications.type schema check.
const (
	TypeCategoryThreshold  = "category_threshold"
	TypeCategoryOverBudget = "category_over_budget"
	TypeBillDue            = "bill_due"
	TypeBudgetMonthMissing = "budget_month_missing"
	TypeGoalBehindSchedule = "goal_behind_schedule"
	TypeImportNeedsReview  = "import_needs_review"
)

// Types lists every notification type, in preference-listing order.
var Types = []string{
	TypeCategoryThreshold, TypeCategoryOverBudget, TypeBillDue,
	TypeBudgetMonthMissing, TypeGoalBehindSchedule, TypeImportNeedsReview,
}

var validType = map[string]bool{
	TypeCategoryThreshold: true, TypeCategoryOverBudget: true, TypeBillDue: true,
	TypeBudgetMonthMissing: true, TypeGoalBehindSchedule: true, TypeImportNeedsReview: true,
}

var ErrNotFound = errors.New("notification not found")

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// Preference is one type's effective setting: stored row if any, otherwise
// the default (enabled, and the default warning threshold for the category
// threshold type).
type Preference struct {
	Type         string `json:"type"`
	Enabled      bool   `json:"enabled"`
	ThresholdPct *int   `json:"thresholdPct"`
}

// PreferenceInput carries one preference update.
type PreferenceInput struct {
	Type         string `json:"type"`
	Enabled      bool   `json:"enabled"`
	ThresholdPct *int   `json:"thresholdPct"`
}

func (in *PreferenceInput) validate() error {
	if !validType[in.Type] {
		return ValidationError("unknown notification type")
	}
	if in.ThresholdPct != nil && (*in.ThresholdPct < 1 || *in.ThresholdPct > 100) {
		return ValidationError("thresholdPct must be between 1 and 100")
	}
	return nil
}

// ListPage is one page of notifications plus the user's unread count.
type ListPage struct {
	Notifications []db.Notification
	UnreadCount   int64
	Limit         int32
	Offset        int32
}

// Service implements the notification storage and preference logic.
type Service struct {
	q *db.Queries
}

func NewService(q *db.Queries) *Service {
	return &Service{q: q}
}

// List returns one page of the user's notifications, newest first.
func (s *Service) List(ctx context.Context, userID uuid.UUID, limit, offset int32) (ListPage, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.q.ListNotificationsByUser(ctx, db.ListNotificationsByUserParams{
		UserID: userID, Limit: limit, Offset: offset,
	})
	if err != nil {
		return ListPage{}, fmt.Errorf("list notifications: %w", err)
	}
	if rows == nil {
		rows = []db.Notification{}
	}
	unread, err := s.q.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return ListPage{}, fmt.Errorf("count unread notifications: %w", err)
	}
	return ListPage{Notifications: rows, UnreadCount: unread, Limit: limit, Offset: offset}, nil
}

// UnreadCount returns the number of unread notifications.
func (s *Service) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	n, err := s.q.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return n, nil
}

// MarkRead marks one notification read; already-read is a no-op.
func (s *Service) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.q.MarkNotificationRead(ctx, db.MarkNotificationReadParams{ID: id, UserID: userID}); err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	return nil
}

// MarkAllRead marks every unread notification read.
func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	if err := s.q.MarkAllNotificationsRead(ctx, userID); err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	return nil
}

// Preferences returns the effective setting for every notification type,
// merging stored rows over the defaults.
func (s *Service) Preferences(ctx context.Context, userID uuid.UUID) ([]Preference, error) {
	stored, err := loadPreferences(ctx, s.q, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Preference, 0, len(Types))
	for _, typ := range Types {
		out = append(out, effectivePreference(typ, stored))
	}
	return out, nil
}

// SetPreference upserts one type's setting and returns its effective value.
func (s *Service) SetPreference(ctx context.Context, userID uuid.UUID, in PreferenceInput) (Preference, error) {
	if err := in.validate(); err != nil {
		return Preference{}, err
	}
	var pct *int16
	if in.ThresholdPct != nil {
		v := int16(*in.ThresholdPct)
		pct = &v
	}
	row, err := s.q.UpsertNotificationPreference(ctx, db.UpsertNotificationPreferenceParams{
		UserID: userID, Type: in.Type, Enabled: in.Enabled, ThresholdPct: pct,
	})
	if err != nil {
		return Preference{}, fmt.Errorf("upsert notification preference: %w", err)
	}
	return effectivePreference(in.Type, map[string]db.NotificationPreference{in.Type: row}), nil
}

func loadPreferences(ctx context.Context, q *db.Queries, userID uuid.UUID) (map[string]db.NotificationPreference, error) {
	rows, err := q.ListNotificationPreferencesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list notification preferences: %w", err)
	}
	m := make(map[string]db.NotificationPreference, len(rows))
	for _, r := range rows {
		m[r.Type] = r
	}
	return m, nil
}

// effectivePreference applies defaults: every type is enabled until disabled,
// and the category threshold type defaults to the budgetmath warning
// threshold.
func effectivePreference(typ string, stored map[string]db.NotificationPreference) Preference {
	p := Preference{Type: typ, Enabled: true}
	if row, ok := stored[typ]; ok {
		p.Enabled = row.Enabled
		if row.ThresholdPct != nil {
			v := int(*row.ThresholdPct)
			p.ThresholdPct = &v
		}
	}
	if typ == TypeCategoryThreshold && p.ThresholdPct == nil {
		v := budgetmath.DefaultWarningThresholdPct
		p.ThresholdPct = &v
	}
	return p
}

// enabled reports whether a type should notify (default true).
func enabled(stored map[string]db.NotificationPreference, typ string) bool {
	row, ok := stored[typ]
	return !ok || row.Enabled
}

// warningThreshold returns the user's configured category warning threshold
// or the budgetmath default.
func warningThreshold(stored map[string]db.NotificationPreference) int {
	if row, ok := stored[TypeCategoryThreshold]; ok && row.ThresholdPct != nil {
		return int(*row.ThresholdPct)
	}
	return budgetmath.DefaultWarningThresholdPct
}
