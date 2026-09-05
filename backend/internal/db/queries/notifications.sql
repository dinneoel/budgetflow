-- name: CreateNotification :one
INSERT INTO notifications (user_id, type, title, body, action_url, dedupe_key)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, dedupe_key) WHERE read_at IS NULL DO NOTHING
RETURNING *;

-- name: ListNotificationsByUser :many
SELECT * FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :exec
UPDATE notifications SET read_at = now() WHERE id = $1 AND user_id = $2 AND read_at IS NULL;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL;

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preferences (user_id, type, enabled, threshold_pct)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, type)
DO UPDATE SET enabled = EXCLUDED.enabled, threshold_pct = EXCLUDED.threshold_pct, updated_at = now()
RETURNING *;

-- name: ListNotificationPreferencesByUser :many
SELECT * FROM notification_preferences WHERE user_id = $1 ORDER BY type;
