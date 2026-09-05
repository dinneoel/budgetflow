// Package auth implements sign-up/sign-in with argon2id hashing, DB-backed
// sessions, password reset, profile management, and CSRF token derivation.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // embed tz database so time-zone validation works in minimal containers

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"budgetflow/internal/audit"
	"budgetflow/internal/db"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountLocked      = errors.New("account temporarily locked due to repeated failed sign-ins")
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrUnauthenticated    = errors.New("unauthenticated")
)

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// dummyHash keeps sign-in timing comparable whether or not the email exists.
var dummyHash, _ = HashPassword("timing-equalizer-dummy")

// Service implements authentication and profile business logic over the
// generated queries.
type Service struct {
	q      *db.Queries
	mailer Mailer
	log    *slog.Logger
	secret []byte

	// Tunable policies, set to production defaults by NewService.
	SessionTTL      time.Duration
	ResetTokenTTL   time.Duration
	MaxFailedLogins int32
	LockoutDuration time.Duration
}

func NewService(q *db.Queries, mailer Mailer, log *slog.Logger, secret string) *Service {
	return &Service{
		q:               q,
		mailer:          mailer,
		log:             log,
		secret:          []byte(secret),
		SessionTTL:      30 * 24 * time.Hour,
		ResetTokenTTL:   time.Hour,
		MaxFailedLogins: 5,
		LockoutDuration: 15 * time.Minute,
	}
}

// newToken returns a fresh random credential and its storage hash. Only the
// hash is persisted, so a database leak does not leak usable tokens.
func newToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// SignUp registers a user and opens their first session. It returns the raw
// session token for the cookie.
func (s *Service) SignUp(ctx context.Context, email, password, name, currency, userAgent, ip string) (db.User, string, error) {
	email = strings.TrimSpace(email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		return db.User{}, "", ValidationError("invalid email address")
	}
	if len(password) < 8 {
		return db.User{}, "", ValidationError("password must be at least 8 characters")
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "USD"
	}
	if !currencyRe.MatchString(currency) {
		return db.User{}, "", ValidationError("currency must be a 3-letter ISO code")
	}

	hash, err := HashPassword(password)
	if err != nil {
		return db.User{}, "", err
	}
	user, err := s.q.CreateUser(ctx, db.CreateUserParams{
		Email:           email,
		PasswordHash:    hash,
		Name:            strings.TrimSpace(name),
		DefaultCurrency: currency,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.User{}, "", ErrEmailTaken
		}
		return db.User{}, "", fmt.Errorf("create user: %w", err)
	}
	if err := audit.Record(ctx, s.q, &user.ID, audit.EventSignUp, "user", []uuid.UUID{user.ID}, map[string]string{"ip": ip}); err != nil {
		return db.User{}, "", err
	}
	token, err := s.createSession(ctx, user.ID, userAgent, ip)
	if err != nil {
		return db.User{}, "", err
	}
	return user, token, nil
}

// SignIn verifies credentials, enforcing account lockout after repeated
// failures, and opens a session.
func (s *Service) SignIn(ctx context.Context, email, password, userAgent, ip string) (db.User, string, error) {
	user, err := s.q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = VerifyPassword(dummyHash, password)
		return db.User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return db.User{}, "", fmt.Errorf("look up user: %w", err)
	}
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		return db.User{}, "", ErrAccountLocked
	}

	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return db.User{}, "", fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		attempts, err := s.q.RecordFailedLogin(ctx, user.ID)
		if err != nil {
			return db.User{}, "", fmt.Errorf("record failed login: %w", err)
		}
		if attempts >= s.MaxFailedLogins {
			until := time.Now().Add(s.LockoutDuration)
			if err := s.q.LockUser(ctx, db.LockUserParams{ID: user.ID, LockedUntil: &until}); err != nil {
				return db.User{}, "", fmt.Errorf("lock account: %w", err)
			}
			if err := audit.Record(ctx, s.q, &user.ID, audit.EventAccountLocked, "user", []uuid.UUID{user.ID},
				map[string]any{"ip": ip, "failed_attempts": attempts}); err != nil {
				return db.User{}, "", err
			}
			return db.User{}, "", ErrAccountLocked
		}
		return db.User{}, "", ErrInvalidCredentials
	}

	if err := s.q.ResetFailedLogins(ctx, user.ID); err != nil {
		return db.User{}, "", fmt.Errorf("reset failed logins: %w", err)
	}
	token, err := s.createSession(ctx, user.ID, userAgent, ip)
	if err != nil {
		return db.User{}, "", err
	}
	if err := audit.Record(ctx, s.q, &user.ID, audit.EventSignIn, "user", []uuid.UUID{user.ID}, map[string]string{"ip": ip}); err != nil {
		return db.User{}, "", err
	}
	return user, token, nil
}

func (s *Service) createSession(ctx context.Context, userID uuid.UUID, userAgent, ip string) (string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = s.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: hash,
		UserAgent: userAgent,
		IpAddress: ip,
		ExpiresAt: time.Now().Add(s.SessionTTL),
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return raw, nil
}

// Authenticate resolves a raw session token to its user and session row.
func (s *Service) Authenticate(ctx context.Context, rawToken string) (db.User, db.Session, error) {
	sess, err := s.q.GetSessionByTokenHash(ctx, hashToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, db.Session{}, ErrUnauthenticated
	}
	if err != nil {
		return db.User{}, db.Session{}, fmt.Errorf("look up session: %w", err)
	}
	user, err := s.q.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return db.User{}, db.Session{}, fmt.Errorf("load session user: %w", err)
	}
	return user, sess, nil
}

// SignOut ends one session.
func (s *Service) SignOut(ctx context.Context, sess db.Session) error {
	if err := s.q.DeleteSession(ctx, sess.ID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return audit.Record(ctx, s.q, &sess.UserID, audit.EventSignOut, "session", []uuid.UUID{sess.ID}, nil)
}

// SignOutAll ends every session the user has, on all devices.
func (s *Service) SignOutAll(ctx context.Context, userID uuid.UUID) error {
	if err := s.q.DeleteSessionsByUser(ctx, userID); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return audit.Record(ctx, s.q, &userID, audit.EventSignOutAll, "user", []uuid.UUID{userID}, nil)
}

// ListSessions returns the user's active sessions.
func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]db.Session, error) {
	return s.q.ListSessionsByUser(ctx, userID)
}

// RequestPasswordReset issues a reset token and hands it to the mailer. It
// succeeds silently for unknown emails so the endpoint cannot be used to
// probe which addresses are registered.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	user, err := s.q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("look up user: %w", err)
	}
	raw, hash, err := newToken()
	if err != nil {
		return err
	}
	_, err = s.q.CreatePasswordResetToken(ctx, db.CreatePasswordResetTokenParams{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(s.ResetTokenTTL),
	})
	if err != nil {
		return fmt.Errorf("create reset token: %w", err)
	}
	if err := s.mailer.SendPasswordReset(ctx, user.Email, raw); err != nil {
		return fmt.Errorf("send reset email: %w", err)
	}
	return audit.Record(ctx, s.q, &user.ID, audit.EventPasswordResetRequested, "user", []uuid.UUID{user.ID}, nil)
}

// ResetPassword consumes a reset token, sets the new password, and signs the
// user out everywhere.
func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if len(newPassword) < 8 {
		return ValidationError("password must be at least 8 characters")
	}
	tok, err := s.q.GetPasswordResetToken(ctx, hashToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("look up reset token: %w", err)
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{ID: tok.UserID, PasswordHash: hash}); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if err := s.q.MarkPasswordResetTokenUsed(ctx, tok.ID); err != nil {
		return fmt.Errorf("mark token used: %w", err)
	}
	if err := s.q.ResetFailedLogins(ctx, tok.UserID); err != nil {
		return fmt.Errorf("reset failed logins: %w", err)
	}
	if err := s.q.DeleteSessionsByUser(ctx, tok.UserID); err != nil {
		return fmt.Errorf("invalidate sessions: %w", err)
	}
	return audit.Record(ctx, s.q, &tok.UserID, audit.EventPasswordReset, "user", []uuid.UUID{tok.UserID}, nil)
}

// ProfileUpdate is a full replacement of the editable profile fields.
type ProfileUpdate struct {
	Name            string `json:"name"`
	Locale          string `json:"locale"`
	TimeZone        string `json:"timeZone"`
	FirstDayOfWeek  int16  `json:"firstDayOfWeek"`
	DefaultCurrency string `json:"defaultCurrency"`
}

// UpdateProfile validates and persists profile settings.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, p ProfileUpdate) (db.User, error) {
	if p.Locale == "" {
		return db.User{}, ValidationError("locale is required")
	}
	if _, err := time.LoadLocation(p.TimeZone); err != nil || p.TimeZone == "" {
		return db.User{}, ValidationError("invalid time zone")
	}
	if p.FirstDayOfWeek < 0 || p.FirstDayOfWeek > 6 {
		return db.User{}, ValidationError("first day of week must be 0 (Sunday) through 6 (Saturday)")
	}
	p.DefaultCurrency = strings.ToUpper(strings.TrimSpace(p.DefaultCurrency))
	if !currencyRe.MatchString(p.DefaultCurrency) {
		return db.User{}, ValidationError("currency must be a 3-letter ISO code")
	}
	user, err := s.q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
		ID:              userID,
		Name:            strings.TrimSpace(p.Name),
		Locale:          p.Locale,
		TimeZone:        p.TimeZone,
		FirstDayOfWeek:  p.FirstDayOfWeek,
		DefaultCurrency: p.DefaultCurrency,
	})
	if err != nil {
		return db.User{}, fmt.Errorf("update profile: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, audit.EventProfileUpdated, "user", []uuid.UUID{userID}, p); err != nil {
		return db.User{}, err
	}
	return user, nil
}

// CSRFToken derives the CSRF token bound to a session token. It is stateless:
// HMAC(secret, session token), so no extra storage or rotation is needed and
// the token dies with the session.
func (s *Service) CSRFToken(sessionToken string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("csrf:" + sessionToken))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyCSRF checks a client-provided CSRF token against the session.
func (s *Service) VerifyCSRF(sessionToken, provided string) bool {
	return hmac.Equal([]byte(s.CSRFToken(sessionToken)), []byte(provided))
}
