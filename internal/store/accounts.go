package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrEmailTaken is returned when an address is already registered.
//
// The caller must not turn this into a distinguishable response: replying
// differently to a taken address than to a fresh one makes registration a user
// enumeration oracle.
var ErrEmailTaken = errors.New("store: email already registered")

// Account is the identity half of a user row.
type Account struct {
	ID            int64
	Email         string
	EmailVerified bool
}

// Credentials is what a password check needs and nothing more.
//
// Hash is nil for an OAuth-only account, which is a real state and not an
// error: the caller must answer it with the same generic response it gives a
// wrong password.
type Credentials struct {
	UserID int64
	Hash   *string
}

// CreateUser registers an address, or reports that it is taken.
func CreateUser(ctx context.Context, pool *pgxpool.Pool, email, hash string) (int64, error) {
	var userID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 ON CONFLICT (email) DO NOTHING
		 RETURNING id`, email, hash).Scan(&userID)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrEmailTaken
	}
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return userID, nil
}

// FindCredentials looks up a password hash by address.
func FindCredentials(ctx context.Context, pool *pgxpool.Pool, email string) (Credentials, error) {
	var c Credentials
	err := pool.QueryRow(ctx,
		`SELECT id, password_hash FROM users WHERE email = $1 AND deleted_at IS NULL`,
		email).Scan(&c.UserID, &c.Hash)

	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, fmt.Errorf("find credentials: %w", err)
	}
	return c, nil
}

// SetPasswordHash replaces a stored hash, for transparent rehashing.
func SetPasswordHash(ctx context.Context, pool *pgxpool.Pool, userID int64, hash string) error {
	_, err := pool.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID, hash)
	if err != nil {
		return fmt.Errorf("set password hash: %w", err)
	}
	return nil
}

// LoadAccount reads the identity fields for a signed-in user.
func LoadAccount(ctx context.Context, pool *pgxpool.Pool, userID int64) (Account, error) {
	var a Account
	var verifiedAt *time.Time
	err := pool.QueryRow(ctx,
		`SELECT id, email, email_verified_at FROM users WHERE id = $1 AND deleted_at IS NULL`,
		userID).Scan(&a.ID, &a.Email, &verifiedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("load account: %w", err)
	}
	a.EmailVerified = verifiedAt != nil
	return a, nil
}

// RecordVisit advances last_active_at, and previous_visit_at when the gap since
// the last activity is long enough to count as a separate visit.
//
// previous_visit_at is what "new since you were last here" is measured from, so
// it must not move on every request — otherwise the answer is always "nothing
// new", which is the bug this shape prevents.
//
// Rate-limited to one write per minute per user in the WHERE clause rather than
// in Go: the cheapest place to discard a redundant write is the one that does
// not need a round trip to decide.
func RecordVisit(ctx context.Context, pool *pgxpool.Pool, userID int64, visitGap time.Duration, log *slog.Logger) {
	_, err := pool.Exec(ctx, `
		UPDATE users
		   SET previous_visit_at = CASE
		         WHEN last_active_at IS NULL
		           OR last_active_at < now() - $2::interval
		         THEN COALESCE(last_active_at, now())
		         ELSE previous_visit_at
		       END,
		       last_active_at = now()
		 WHERE id = $1
		   AND (last_active_at IS NULL OR last_active_at < now() - interval '1 minute')`,
		userID, visitGap.String())

	// Best effort by design: failing to record a visit must never fail the
	// request the user actually made.
	if err != nil && log != nil {
		log.WarnContext(ctx, "could not record visit", "error", err)
	}
}
