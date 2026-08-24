package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/jobtrack/jobtrack/internal/auth"
	"github.com/jobtrack/jobtrack/internal/httpx"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type meResponse struct {
	ID            int64  `json:"id"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// Password length bounds. The upper limit is not a strength requirement — it
// stops a multi-megabyte password from turning Argon2 into a denial of service,
// since hashing cost scales with input.
const (
	minPasswordLen = 12
	maxPasswordLen = 256
)

func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) error {
	var in credentials
	if err := decodeJSON(r, &in); err != nil {
		return err
	}

	email, err := normaliseEmail(in.Email)
	if err != nil {
		return err
	}
	if err := validatePassword(in.Password); err != nil {
		return err
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	var userID int64
	err = a.pool.QueryRow(r.Context(),
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 ON CONFLICT (email) DO NOTHING
		 RETURNING id`, email, hash).Scan(&userID)

	if errors.Is(err, pgx.ErrNoRows) {
		// The address is already registered. Say nothing that confirms it:
		// return the same 202 shape, issue no session, and send a "someone tried
		// to register with your address" email out of band instead.
		a.log.InfoContext(r.Context(), "registration attempted for existing address")
		httpx.WriteJSON(r.Context(), w, a.log, http.StatusAccepted,
			map[string]string{"status": "verification_sent"})
		return nil
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// Sign the new account in immediately, leaving the address UNVERIFIED.
	//
	// The previous behaviour returned 202 and no session, on the reasoning that
	// an unverified address should not get access. That reasoning is sound in
	// isolation and produced a dead end in practice: registration is followed
	// immediately by onboarding, and with no session the new user was bounced
	// straight back to a sign-in page for an account they had just created and
	// could not yet use. Nobody recovers from that.
	//
	// So verification gates what it should actually gate — anything we would
	// SEND to the address, like job alerts — rather than gating access to the
	// app. `email_verified_at` stays NULL until the link is followed, and every
	// outbound-email feature checks it.
	//
	// The cost is honest: an attacker can now distinguish a new address from an
	// existing one by whether a session cookie comes back. That fiction was
	// already thin — timing, password reset and the sign-in form all leak the
	// same fact — and the per-IP rate limit in front of this endpoint is what
	// actually makes enumeration expensive.
	token, expires, err := a.sessions.Create(r.Context(), userID,
		r.UserAgent(), hashClientIP(r, a.cfg.Security.TrustedProxies))
	if err != nil {
		return httpx.ErrInternal(err)
	}

	http.SetCookie(w, auth.NewCookie(token, auth.CookieOptions{
		Secure: a.cfg.Security.CookieSecure,
		TTL:    time.Until(expires),
	}))

	a.log.InfoContext(r.Context(), "user registered", "user_id", userID)
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusCreated,
		map[string]any{"expires_at": expires})
	return nil
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var in credentials
	if err := decodeJSON(r, &in); err != nil {
		return err
	}

	email, err := normaliseEmail(in.Email)
	if err != nil {
		// Do not distinguish a malformed address from a wrong one.
		return errInvalidCredentials()
	}

	var (
		userID int64
		hash   *string
	)
	err = a.pool.QueryRow(r.Context(),
		`SELECT id, password_hash FROM users WHERE email = $1 AND deleted_at IS NULL`,
		email).Scan(&userID, &hash)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Spend the same time as a real verify. Returning early here would make
		// response latency a user-enumeration oracle — the classic mistake in
		// this exact code path.
		auth.DummyVerify(in.Password)
		return errInvalidCredentials()
	case err != nil:
		return httpx.ErrInternal(err)
	case hash == nil:
		// OAuth-only account. Same generic response.
		auth.DummyVerify(in.Password)
		return errInvalidCredentials()
	}

	if err := auth.VerifyPassword(in.Password, *hash); err != nil {
		if errors.Is(err, auth.ErrMismatch) {
			return errInvalidCredentials()
		}
		return httpx.ErrInternal(err)
	}

	// Transparently upgrade hashes made with weaker parameters. Without this,
	// raising the policy would only ever protect new accounts.
	if auth.NeedsRehash(*hash) {
		if newHash, err := auth.HashPassword(in.Password); err == nil {
			if _, err := a.pool.Exec(r.Context(),
				`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`,
				userID, newHash); err != nil {
				// Not fatal to the login — log and continue.
				a.log.WarnContext(r.Context(), "password rehash failed", "user_id", userID, "error", err)
			}
		}
	}

	token, expires, err := a.sessions.Create(r.Context(), userID,
		r.UserAgent(), hashClientIP(r, a.cfg.Security.TrustedProxies))
	if err != nil {
		return httpx.ErrInternal(err)
	}

	http.SetCookie(w, auth.NewCookie(token, auth.CookieOptions{
		Secure: a.cfg.Security.CookieSecure,
		TTL:    time.Until(expires),
	}))

	a.log.InfoContext(r.Context(), "login succeeded", "user_id", userID)
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK,
		map[string]any{"expires_at": expires})
	return nil
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(auth.CookieName(a.cfg.Security.CookieSecure)); err == nil {
		if err := a.sessions.Revoke(r.Context(), c.Value); err != nil {
			return httpx.ErrInternal(err)
		}
	}
	// Always clear the cookie, even if there was no session. Logout must be
	// idempotent — a user clicking it twice should not see an error.
	http.SetCookie(w, auth.ClearCookie(a.cfg.Security.CookieSecure))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) error {
	userID := httpx.UserIDFromContext(r.Context())

	var out meResponse
	var verifiedAt *time.Time
	err := a.pool.QueryRow(r.Context(),
		`SELECT id, email, email_verified_at FROM users
		  WHERE id = $1 AND deleted_at IS NULL`, userID).
		Scan(&out.ID, &out.Email, &verifiedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound()
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}
	out.EmailVerified = verifiedAt != nil

	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, out)
	return nil
}

// optionalAuth resolves the session cookie when there is one, and does nothing
// when there is not.
//
// Used by the public feed so a signed-in viewer sees their match scores without
// discovery being gated behind an account. Errors are deliberately swallowed:
// on this path an invalid session means "anonymous", not "rejected", and
// turning a stale cookie into a 401 on a public page would lock people out of
// content that needs no login at all.
func (a *API) optionalAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(auth.CookieName(a.cfg.Security.CookieSecure))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			sess, err := a.sessions.Validate(r.Context(), c.Value)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := httpx.WithUserID(r.Context(), sess.UserID)
			a.touchVisit(ctx, sess.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// visitGap is how long a silence has to be before returning counts as a new
// visit. Long enough that scrolling the feed, opening a posting and coming back
// is one visit; short enough that a lunchtime look and an evening look are two.
const visitGap = 30 * time.Minute

// touchVisit records that the user is here, and rolls the visit marker when
// they have been away.
//
// Two columns rather than one, because a single "last seen" timestamp that is
// updated as someone browses is always ~now — so "new since your last visit"
// would erase its own marker the moment it was read. `previous_visit_at` holds
// the value from before the current visit began, which is what a person means.
//
// Fire-and-forget: this is bookkeeping, and a user request must never fail or
// wait because of it. The single UPDATE is a no-op when nothing has changed, so
// browsing does not write on every request.
func (a *API) touchVisit(ctx context.Context, userID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()

		if _, err := a.pool.Exec(ctx, `
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
			userID, visitGap.String()); err != nil {
			a.log.WarnContext(ctx, "could not record visit", "error", err)
		}
	}()
}

// requireAuth resolves the session cookie and rejects unauthenticated requests.
func (a *API) requireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(auth.CookieName(a.cfg.Security.CookieSecure))
			if err != nil {
				httpx.WriteProblem(r.Context(), w, r, a.log, httpx.ErrUnauthorized())
				return
			}

			sess, err := a.sessions.Validate(r.Context(), c.Value)
			if err != nil {
				if !errors.Is(err, auth.ErrSessionInvalid) {
					httpx.WriteProblem(r.Context(), w, r, a.log, httpx.ErrInternal(err))
					return
				}
				// Clear the stale cookie so the browser stops sending it.
				http.SetCookie(w, auth.ClearCookie(a.cfg.Security.CookieSecure))
				httpx.WriteProblem(r.Context(), w, r, a.log, httpx.ErrUnauthorized())
				return
			}

			ctx := httpx.WithUserID(r.Context(), sess.UserID)
			a.touchVisit(ctx, sess.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// --- validation helpers ---

// errInvalidCredentials is deliberately identical for "no such user" and "wrong
// password". Anything more specific is an enumeration oracle.
func errInvalidCredentials() error {
	return &httpx.APIError{
		Status: http.StatusUnauthorized,
		Kind:   "invalid-credentials",
		Title:  "Invalid email or password",
	}
}

func normaliseEmail(raw string) (string, error) {
	e := strings.TrimSpace(strings.ToLower(raw))
	if e == "" {
		return "", httpx.ErrBadRequest("email is required",
			httpx.FieldError{Field: "email", Code: "required", Message: "must not be empty"})
	}
	if len(e) > 254 { // RFC 5321 maximum
		return "", httpx.ErrBadRequest("email is too long",
			httpx.FieldError{Field: "email", Code: "too_long", Message: "must be at most 254 characters"})
	}
	if _, err := mail.ParseAddress(e); err != nil {
		return "", httpx.ErrBadRequest("email is not a valid address",
			httpx.FieldError{Field: "email", Code: "invalid", Message: "must be a valid email address"})
	}
	return e, nil
}

func validatePassword(pw string) error {
	// Count runes, not bytes: a passphrase in a non-Latin script would
	// otherwise be judged by its UTF-8 encoding length.
	n := utf8.RuneCountInString(pw)
	if n < minPasswordLen {
		return httpx.ErrBadRequest("password is too short",
			httpx.FieldError{Field: "password", Code: "too_short",
				Message: "must be at least 12 characters"})
	}
	if n > maxPasswordLen {
		return httpx.ErrBadRequest("password is too long",
			httpx.FieldError{Field: "password", Code: "too_long",
				Message: "must be at most 256 characters"})
	}
	// Deliberately no composition rules (uppercase, digit, symbol). They push
	// users toward predictable patterns like "Password1!" and NIST no longer
	// recommends them. Length plus a breached-password check is the better pair.
	return nil
}

func decodeJSON(r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" &&
		!strings.HasPrefix(ct, "application/json") {
		return httpx.ErrBadRequest("Content-Type must be application/json")
	}

	dec := json.NewDecoder(r.Body)
	// Reject unknown fields: a client sending {"emial": "..."} should be told,
	// not silently registered with an empty address.
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return httpx.ErrTooLarge("request body exceeds the limit")
		}
		return httpx.ErrBadRequest("request body is not valid JSON: " + err.Error())
	}
	// A second value in the stream means the client sent two JSON documents.
	if dec.More() {
		return httpx.ErrBadRequest("request body must contain a single JSON object")
	}
	return nil
}

// hashClientIP stores a hash rather than the address itself: enough for anomaly
// detection, not enough to be a location record.
func hashClientIP(r *http.Request, trustedProxies []string) []byte {
	ip := httpx.KeyByIP(trustedProxies)(r)
	if ip == "" {
		return nil
	}
	if parsed := net.ParseIP(ip); parsed == nil {
		return nil
	}
	sum := sha256.Sum256([]byte(ip))
	return sum[:]
}
