// Package audit records security- and data-relevant events to the
// audit_events table. Events are append-only; user_id is nullable so the
// final event of an account deletion survives the cascade.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"budgetflow/internal/db"
)

// Event types recorded by the auth layer. Feature packages define their own
// constants alongside their bulk operations.
const (
	EventSignUp                 = "sign_up"
	EventSignIn                 = "sign_in"
	EventSignOut                = "sign_out"
	EventSignOutAll             = "sign_out_all"
	EventAccountLocked          = "account_locked"
	EventPasswordResetRequested = "password_reset_requested"
	EventPasswordReset          = "password_reset"
	EventProfileUpdated         = "profile_updated"
)

// Record writes one audit event. payload may be nil or any JSON-marshalable
// value; entityIDs may be nil.
func Record(ctx context.Context, q *db.Queries, userID *uuid.UUID, eventType, entityType string, entityIDs []uuid.UUID, payload any) error {
	body := []byte("{}")
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal audit payload: %w", err)
		}
		body = b
	}
	if entityIDs == nil {
		entityIDs = []uuid.UUID{}
	}
	_, err := q.CreateAuditEvent(ctx, db.CreateAuditEventParams{
		UserID:     userID,
		EventType:  eventType,
		EntityType: entityType,
		EntityIds:  entityIDs,
		Payload:    body,
	})
	if err != nil {
		return fmt.Errorf("record audit event %s: %w", eventType, err)
	}
	return nil
}
