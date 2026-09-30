//go:build integration

package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/ergodicregulus/jobtrack/internal/config"
	"github.com/ergodicregulus/jobtrack/internal/mail"
	"github.com/ergodicregulus/jobtrack/internal/migrate"
	"github.com/ergodicregulus/jobtrack/internal/store"
	"github.com/ergodicregulus/jobtrack/migrations"
)

// fakeSender records what would have been emailed.
type fakeSender struct{ sent []mail.Message }

func (f *fakeSender) Send(_ context.Context, m mail.Message) error {
	f.sent = append(f.sent, m)
	return nil
}

// The digest had no test, and the first one written found that it did not read
// the saved search at all: every subscriber was sent the corpus's newest roles,
// counted across the whole corpus, under their own search's name. This runs the
// worker end to end against a real schema, with only the SMTP hop faked.
func TestSendDigests_SendsOnlyWhatTheSearchMatches(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	reader := seedReader(t, pool, "reader@example.test", true)
	_, err := store.CreateSavedSearch(ctx, pool, reader, "Ashby roles", "vendor=ashby", false)
	must(t, err)
	_, err = store.CreateSavedSearch(ctx, pool, reader, "Lever roles", "vendor=lever", false)
	must(t, err)
	// A matching search from someone who never opted in: no email, whatever matches.
	silent := seedReader(t, pool, "silent@example.test", false)
	_, err = store.CreateSavedSearch(ctx, pool, silent, "Ashby roles", "vendor=ashby", false)
	must(t, err)

	seedJob(t, pool, "ashby", "Platform Engineer", "now()")
	seedJob(t, pool, "greenhouse", "Payments Engineer", "now()")
	seedJob(t, pool, "ashby", "Old Ashby Role", "now() - interval '30 days'")

	fake := &fakeSender{}
	cfg := &config.Config{Email: config.Email{
		Enabled: true, Host: "smtp.invalid", From: "digest@jobtrack.test",
		BaseURL: "https://jobtrack.test/", UnsubscribeSecret: "test-secret",
	}}
	w := &SendDigestsWorker{Deps: &Deps{
		Pool: pool, Cfg: cfg, Mailer: fake,
		Log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}}
	job := &river.Job[SendDigestsArgs]{Args: SendDigestsArgs{Interval: time.Hour}}

	must(t, w.Work(ctx, job))

	if len(fake.sent) != 1 {
		t.Fatalf("sent %d emails, want 1 (Ashby roles only: Lever matches nothing new, "+
			"and the second reader never opted in)", len(fake.sent))
	}
	m := fake.sent[0]
	if m.To != "reader@example.test" {
		t.Errorf("sent to %q", m.To)
	}
	if want := `1 new role for "Ashby roles"`; m.Subject != want {
		t.Errorf("subject %q, want %q", m.Subject, want)
	}
	for _, body := range []string{m.Text, m.HTML} {
		if !strings.Contains(body, "Platform Engineer") {
			t.Errorf("the matching role is missing:\n%s", body)
		}
		if strings.Contains(body, "Payments Engineer") {
			t.Errorf("a Greenhouse role reached an Ashby-only search:\n%s", body)
		}
		if strings.Contains(body, "Old Ashby Role") {
			t.Errorf("a role older than the search's boundary was sent as new:\n%s", body)
		}
	}

	u, err := url.Parse(m.Unsubscribe)
	must(t, err)
	if u.Host != "jobtrack.test" || u.Path != "/unsubscribe" {
		t.Errorf("unsubscribe link %q is not on the site's own /unsubscribe", m.Unsubscribe)
	}
	if !mail.VerifyUnsubscribe("test-secret", reader, u.Query().Get("t")) ||
		u.Query().Get("u") != fmt.Sprint(reader) {
		t.Errorf("unsubscribe link %q does not verify for its reader", m.Unsubscribe)
	}

	// Inside the interval nothing goes again: a duplicate digest is the thing
	// that makes people unsubscribe.
	must(t, w.Work(ctx, job))
	if len(fake.sent) != 1 {
		t.Fatalf("a second run inside the interval sent %d more", len(fake.sent)-1)
	}
}

// Due again, a search must not resend what the last digest already named.
func TestSendDigests_DoesNotResendAcrossDigests(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	reader := seedReader(t, pool, "reader@example.test", true)
	_, err := store.CreateSavedSearch(ctx, pool, reader, "Ashby roles", "vendor=ashby", false)
	must(t, err)
	seedJob(t, pool, "ashby", "Platform Engineer", "now()")

	fake := &fakeSender{}
	w := &SendDigestsWorker{Deps: &Deps{
		Pool: pool, Mailer: fake,
		Cfg: &config.Config{Email: config.Email{
			Enabled: true, Host: "smtp.invalid", From: "d@jobtrack.test",
			BaseURL: "https://jobtrack.test", UnsubscribeSecret: "s",
		}},
		Log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}}
	// A zero interval makes every search due on every run.
	job := &river.Job[SendDigestsArgs]{Args: SendDigestsArgs{Interval: time.Nanosecond}}

	must(t, w.Work(ctx, job))
	must(t, w.Work(ctx, job))
	if len(fake.sent) != 1 {
		t.Fatalf("sent %d emails for one new role, want 1 — the second repeated the first", len(fake.sent))
	}

	seedJob(t, pool, "ashby", "Data Engineer", "now()")
	must(t, w.Work(ctx, job))
	if len(fake.sent) != 2 || strings.Contains(fake.sent[1].Text, "Platform Engineer") {
		t.Fatalf("the next digest should name only the newer role; got %d emails", len(fake.sent))
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func seedReader(t *testing.T, pool *pgxpool.Pool, email string, optedIn bool) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := store.CreateUser(ctx, pool, email, "x", nil, "")
	must(t, err)
	if optedIn {
		must(t, store.GrantConsent(ctx, pool, id, "digest_email", nil, ""))
	}
	return id
}

// seedJob inserts a live posting on its own company and board. postedAt is SQL,
// so the boundary is set by the database clock the query compares against.
func seedJob(t *testing.T, pool *pgxpool.Pool, vendor, title, postedAt string) {
	t.Helper()
	slug := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	_, err := pool.Exec(context.Background(), `
		WITH c AS (
		  INSERT INTO companies (name, slug) VALUES ($2, $3) RETURNING id
		), s AS (
		  INSERT INTO sources (company_id, vendor, board_token)
		  SELECT id, $1::source_vendor, $3 FROM c RETURNING id, company_id
		)
		INSERT INTO job_postings
			(company_id, source_id, external_id, title, title_normalised, status, apply_url, posted_at)
		SELECT company_id, id, $3, $2, lower($2), 'live', 'https://example.test/' || $3, `+postedAt+`
		  FROM s`, vendor, title, slug)
	must(t, err)
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
	must(t, err)
	defer admin.Close()

	name := fmt.Sprintf("jt_jobs_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, "CREATE DATABASE "+name)
	must(t, err)

	u, err := url.Parse(base)
	must(t, err)
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	must(t, err)
	t.Cleanup(func() {
		pool.Close()
		if a, err := pgxpool.New(context.Background(), base); err == nil {
			defer a.Close()
			_, _ = a.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		}
	})

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	must(t, migrate.New(pool, migrations.FS, log, "test").Up(ctx, 30*time.Second))
	return pool
}
