package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Session rows are keyed by a hash of the token, never the token itself.
//
// Every function here takes an already-hashed id. The hashing lives in
// internal/auth beside the policy that produces the token, and the []byte
// parameter type is what keeps a raw token from being passed here by accident:
// a database dump must not contain anything replayable as a live session, which
// is the same reasoning as not storing plaintext passwords.

// ErrSessionInvalid is returned for a token that is unknown or expired. The two
// are not distinguished, because a caller has nothing different to do about
// them and telling them apart leaks whether a token ever existed.
var ErrSessionInvalid = errors.New("store: session invalid")

// CreateSession records a new session.
func CreateSession(
	ctx context.Context,
	pool *pgxpool.Pool,
	id []byte,
	userID int64,
	expires time.Time,
	userAgent string,
	ipHash []byte,
) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO sessions (id, user_id, expires_at, user_agent, ip_hash)
		 VALUES ($1, $2, $3, $4, $5)`,
		id, userID, expires, userAgent, ipHash)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// TouchSession validates a session and advances last_used_at in one statement.
//
// One statement, not a read then a write: the read alone would let an expired
// session through a concurrent write, and two round trips per authenticated
// request is a cost paid on every single call.
func TouchSession(ctx context.Context, pool *pgxpool.Pool, id []byte) (int64, time.Time, error) {
	var userID int64
	var expires time.Time
	err := pool.QueryRow(ctx, `
		UPDATE sessions
		   SET last_used_at = now()
		 WHERE id = $1 AND expires_at > now()
		 RETURNING user_id, expires_at`, id).Scan(&userID, &expires)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, time.Time{}, ErrSessionInvalid
	}
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("validate session: %w", err)
	}
	return userID, expires, nil
}

// DeleteSession revokes one session. Takes effect on the very next request.
func DeleteSession(ctx context.Context, pool *pgxpool.Pool, id []byte) error {
	_, err := pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// DeleteUserSessions is used on password change and on "sign out everywhere".
func DeleteUserSessions(ctx context.Context, pool *pgxpool.Pool, userID int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

// DeleteExpiredSessions is housekeeping, not a security control: expired rows
// are already rejected by TouchSession.
func DeleteExpiredSessions(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx,
		`DELETE FROM sessions WHERE expires_at < now() - interval '30 days'`)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// TryAdvisoryLock attempts to take a session-scoped Postgres advisory lock,
// which is how the scheduler elects a single leader.
//
// The lock is held for the life of the connection, so the caller must keep it
// and must not return it to the pool while it needs to stay leader.
func TryAdvisoryLock(ctx context.Context, conn *pgxpool.Conn, key int64) (bool, error) {
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired); err != nil {
		return false, fmt.Errorf("try advisory lock: %w", err)
	}
	return acquired, nil
}
