//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/auth"
	"github.com/ergodicregulus/jobtrack/internal/config"
	"github.com/ergodicregulus/jobtrack/internal/mail"
	"github.com/ergodicregulus/jobtrack/internal/migrate"
	"github.com/ergodicregulus/jobtrack/internal/store"
	"github.com/ergodicregulus/jobtrack/migrations"
)

// The digest's consent is the only thing standing between a reader and email
// they did not ask for, and the unsubscribe link is the only way out that needs
// no password. Both run through the real router here, against a real schema.

const unsubSecret = "test-unsubscribe-secret"

func TestConsent_GrantListWithdraw(t *testing.T) {
	h, pool := newTestAPI(t)
	user, cookie := signedIn(t, pool)

	do := func(method, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := do("POST", "/v1/me/consents?purpose=digest_email"); w.Code != http.StatusNoContent {
		t.Fatalf("grant: %d %s", w.Code, w.Body)
	}
	if !digestConsented(t, do("GET", "/v1/me/consents")) {
		t.Fatal("granted, but the list does not show a live digest_email consent")
	}

	// Only the digest is granted here; the others ride on the act they permit.
	if w := do("POST", "/v1/me/consents?purpose=matching"); w.Code != http.StatusBadRequest {
		t.Errorf("granting matching: %d, want 400", w.Code)
	}
	// Withdrawing the account's consent is erasure, not a flag.
	if w := do("DELETE", "/v1/me/consents?purpose=account"); w.Code != http.StatusBadRequest {
		t.Errorf("withdrawing account: %d, want 400", w.Code)
	}

	if w := do("DELETE", "/v1/me/consents?purpose=digest_email"); w.Code != http.StatusNoContent {
		t.Fatalf("withdraw: %d %s", w.Code, w.Body)
	}
	if digestConsented(t, do("GET", "/v1/me/consents")) {
		t.Fatal("withdrawn, but the list still shows a live digest_email consent")
	}
	if n := liveDigestConsents(t, pool, user); n != 0 {
		t.Errorf("%d live digest consents after withdrawal", n)
	}

	r := httptest.NewRequest("POST", "/v1/me/consents?purpose=digest_email", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("grant without a session: %d, want 401", w.Code)
	}
}

func TestUnsubscribe_SignedLinkWithdrawsWithoutASession(t *testing.T) {
	h, pool := newTestAPI(t)
	user, _ := signedIn(t, pool)
	other, _ := signedIn(t, pool)
	for _, id := range []int64{user, other} {
		if err := store.GrantConsent(context.Background(), pool, id, "digest_email", nil, ""); err != nil {
			t.Fatal(err)
		}
	}

	hit := func(method string, id int64, token string) int {
		q := url.Values{"u": {fmt.Sprint(id)}, "t": {token}}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/unsubscribe?"+q.Encode(), nil))
		return w.Code
	}

	// Anyone could unsubscribe anyone if the id alone were enough.
	if code := hit("GET", other, mail.UnsubscribeToken(unsubSecret, user)); code != http.StatusBadRequest {
		t.Errorf("another reader's token: %d, want 400", code)
	}
	if code := hit("GET", user, "forged"); code != http.StatusBadRequest {
		t.Errorf("a forged token: %d, want 400", code)
	}
	if n := liveDigestConsents(t, pool, user); n != 1 {
		t.Fatalf("a refused link changed consent: %d live, want 1", n)
	}

	token := mail.UnsubscribeToken(unsubSecret, user)
	// GET is what a mail client's link sends; POST is RFC 8058 one-click.
	if code := hit("GET", user, token); code != http.StatusOK {
		t.Fatalf("signed link: %d, want 200", code)
	}
	if n := liveDigestConsents(t, pool, user); n != 0 {
		t.Errorf("unsubscribed, but %d live digest consents remain", n)
	}
	if code := hit("POST", user, token); code != http.StatusOK {
		t.Errorf("a second, one-click unsubscribe: %d, want 200 — it is idempotent", code)
	}
	if n := liveDigestConsents(t, pool, other); n != 1 {
		t.Errorf("unsubscribing one reader touched another: %d live, want 1", n)
	}
}

func digestConsented(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("list consents: %d %s", w.Code, w.Body)
	}
	var body struct{ Items []store.Consent }
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, c := range body.Items {
		if c.Purpose == "digest_email" && c.WithdrawnAt == nil {
			return true
		}
	}
	return false
}

func liveDigestConsents(t *testing.T, pool *pgxpool.Pool, user int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM user_consents
		 WHERE user_id = $1 AND purpose = 'digest_email' AND withdrawn_at IS NULL`,
		user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var userSeq int

// signedIn creates a user and a session, and returns the cookie a browser holds.
func signedIn(t *testing.T, pool *pgxpool.Pool) (int64, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	userSeq++
	id, err := store.CreateUser(ctx, pool, fmt.Sprintf("reader%d@example.test", userSeq), "x", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.NewSessionStore(pool, time.Hour).Create(ctx, id, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	return id, &http.Cookie{Name: auth.CookieName(false), Value: token}
}

func newTestAPI(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	pool := newTestDB(t)
	cfg := &config.Config{Env: "dev", Service: "api"}
	cfg.Security.ResumeKey = "test-resume-key-at-least-32-characters"
	cfg.Security.SessionTTL = time.Hour
	cfg.Security.RateLimitPerMinute, cfg.Security.RateLimitBurst = 6000, 1000
	cfg.Email.UnsubscribeSecret = unsubSecret
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(cfg, log, pool)
	if err != nil {
		t.Fatal(err)
	}
	return a.Routes(), pool
}

// newTestDB creates an isolated, fully migrated database for one test.
func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	name := fmt.Sprintf("jt_api_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if a, err := pgxpool.New(context.Background(), base); err == nil {
			defer a.Close()
			_, _ = a.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		}
	})
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := migrate.New(pool, migrations.FS, log, "test").Up(ctx, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	return pool
}
