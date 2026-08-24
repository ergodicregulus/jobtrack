package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// secureCookieName carries the __Host- prefix, which makes the browser
	// enforce Secure, Path=/ and no Domain attribute. That stops a subdomain
	// takeover from setting a session cookie for the parent domain.
	secureCookieName = "__Host-jt_session"

	// plainCookieName is used only when Secure is off, i.e. local development
	// over http. The __Host- prefix REQUIRES Secure, so a __Host- cookie sent
	// over http is silently discarded by the client and login appears to
	// succeed while no session is ever stored. Using a different name in dev
	// is the standard way round it — and keeping the names distinct means a
	// dev cookie can never be mistaken for a production one.
	plainCookieName = "jt_session"

	sessionTokenBytes = 32
)

// CookieName returns the session cookie name for the current security posture.
//
// Callers must use this rather than a constant, because reading and writing
// have to agree on which name is in play.
func CookieName(secure bool) string {
	if secure {
		return secureCookieName
	}
	return plainCookieName
}

var ErrSessionInvalid = errors.New("auth: session is invalid or expired")

// Session is a server-side session record.
//
// Server-side rather than a stateless JWT because revocation must be immediate:
// a user signing out on a shared machine cannot be told to wait for a token to
// expire.
type Session struct {
	UserID    int64
	ExpiresAt time.Time
}

// SessionStore issues and validates sessions.
type SessionStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

func NewSessionStore(pool *pgxpool.Pool, ttl time.Duration) *SessionStore {
	return &SessionStore{pool: pool, ttl: ttl}
}

// Create issues a session and returns the plaintext token for the cookie.
//
// Only the SHA-256 of the token is stored. A database dump therefore cannot be
// replayed as a valid session, which is the same reasoning as not storing
// plaintext passwords.
func (s *SessionStore) Create(ctx context.Context, userID int64, userAgent string, ipHash []byte) (string, time.Time, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	id := hashToken(token)
	expires := time.Now().Add(s.ttl)

	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (id, user_id, expires_at, user_agent, ip_hash)
		 VALUES ($1, $2, $3, $4, $5)`,
		id, userID, expires, truncate(userAgent, 512), ipHash)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: create session: %w", err)
	}
	return token, expires, nil
}

// Validate resolves a token to a session, or returns ErrSessionInvalid.
func (s *SessionStore) Validate(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrSessionInvalid
	}
	id := hashToken(token)

	var sess Session
	err := s.pool.QueryRow(ctx, `
		UPDATE sessions
		   SET last_used_at = now()
		 WHERE id = $1 AND expires_at > now()
		 RETURNING user_id, expires_at`, id).
		Scan(&sess.UserID, &sess.ExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("auth: validate session: %w", err)
	}
	return &sess, nil
}

// Revoke deletes one session. Takes effect on the very next request.
func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, hashToken(token))
	return err
}

// RevokeAllForUser is used on password change and on "sign out everywhere".
func (s *SessionStore) RevokeAllForUser(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

// DeleteExpired is called by the scheduler. Expired rows are already rejected
// by Validate, so this is housekeeping rather than a security control.
func (s *SessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM sessions WHERE expires_at < now() - interval '30 days'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// CookieOptions describes how session cookies are written.
type CookieOptions struct {
	Secure bool
	TTL    time.Duration
}

// NewCookie builds the session cookie.
//
// SameSite=Lax rather than Strict: Strict would drop the session on any
// inbound link, so a user clicking a job alert email would land signed out.
// Lax still blocks the cross-site POST that CSRF requires.
func NewCookie(token string, opts CookieOptions) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName(opts.Secure),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(opts.TTL.Seconds()),
		// No Domain: __Host- prefixed cookies must not set one.
	}
}

// ClearCookie expires the session cookie.
func ClearCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName(secure),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}
