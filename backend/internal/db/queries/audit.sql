-- name: CreateAuditEvent :one
INSERT INTO audit_events (user_id, event_type, entity_type, entity_ids, payload)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListAuditEventsByUser :many
SELECT * FROM audit_events WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;
