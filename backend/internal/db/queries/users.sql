-- name: CreateUser :one
INSERT INTO users (email, password_hash, name, default_currency)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower($1);

-- name: UpdateUserProfile :one
UPDATE users
SET name = $2, locale = $3, time_zone = $4, first_day_of_week = $5, default_currency = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: RecordFailedLogin :one
UPDATE users
SET failed_login_attempts = failed_login_attempts + 1, updated_at = now()
WHERE id = $1
RETURNING failed_login_attempts;

-- name: LockUser :exec
UPDATE users SET locked_until = $2, updated_at = now() WHERE id = $1;

-- name: ResetFailedLogins :exec
UPDATE users SET failed_login_attempts = 0, locked_until = NULL, updated_at = now() WHERE id = $1;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
