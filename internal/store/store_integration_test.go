//go:build integration

package store

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/migrate"
	"github.com/ergodicregulus/jobtrack/migrations"
)

// The store is tested against a real Postgres because there is nothing else to
// test: the behaviour under examination is SQL, and a mock would test the mock.
//
// It matters more here than elsewhere. ADR-0015 chose pgx's RowToStructByName
// over generated code, accepting that a column added to a SELECT without a
// matching struct field fails at runtime rather than at build time. These tests
// are where that failure is supposed to happen.
//
//	make test-integration
//	go test -tags=integration ./internal/store/

// newTestDB creates an isolated, fully migrated database per test.
func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close()

	name := fmt.Sprintf("jt_store_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	pool, err := pgxpool.New(ctx, replaceDBName(base, name))
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		a, err := pgxpool.New(context.Background(), base)
		if err != nil {
			return
		}
		defer a.Close()
		_, _ = a.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := migrate.New(pool, migrations.FS, log, "test").Up(ctx, 30*time.Second); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func replaceDBName(url, name string) string {
	i := strings.LastIndex(url, "/")
	rest := ""
	if q := strings.Index(url[i:], "?"); q >= 0 {
		rest = url[i+q:]
	}
	return url[:i+1] + name + rest
}

// seedUser inserts a user and returns its id.
func seedUser(t *testing.T, pool *pgxpool.Pool, email string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`, email).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// seedPosting inserts a company, source and live posting, and returns the posting id.
func seedPosting(t *testing.T, pool *pgxpool.Pool, title string) int64 {
	t.Helper()
	ctx := context.Background()

	var companyID, sourceID, postingID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug) VALUES ($1, $2) RETURNING id`,
		"Acme "+title, "acme-"+strings.ToLower(strings.ReplaceAll(title, " ", "-"))).Scan(&companyID)
	if err != nil {
		t.Fatalf("seed company: %v", err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO sources (company_id, vendor, board_token) VALUES ($1, 'greenhouse', $2) RETURNING id`,
		companyID, "tok-"+title).Scan(&sourceID)
	if err != nil {
		t.Fatalf("seed source: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO job_postings
			(company_id, source_id, external_id, title, title_normalised, status, apply_url, posted_at)
		VALUES ($1, $2, $3, $4, lower($4), 'live', 'https://example.test/apply', now())
		RETURNING id`, companyID, sourceID, "ext-"+title, title).Scan(&postingID)
	if err != nil {
		t.Fatalf("seed posting: %v", err)
	}
	return postingID
}

// A posting absent from its board must actually close.
//
// This test exists because its absence let a no-op ship for the whole life of
// ReconcileAbsent. The old statement bumped the counter in a data-modifying CTE
// and then updated the same rows again in the outer statement; PostgreSQL
// applies only one modification per row per command, so status='closed' never
// took effect. The function reported closures from the command tag and performed
// none, and 2,020 postings sat absent from their boards — one for 29 consecutive
// polls — still reading as live.
//
// The assertions are deliberately on the ROWS, not on the returned count. The
// returned count is what lied.
func TestReconcileAbsent_ClosesAPostingThatStaysGone(t *testing.T) {
	pool := newTestDB(t)
	ctx := context.Background()

	postingID := seedPosting(t, pool, "Backend Engineer")
	var sourceID int64
	if err := pool.QueryRow(ctx,
		`SELECT source_id FROM job_postings WHERE id = $1`, postingID).Scan(&sourceID); err != nil {
		t.Fatalf("read source: %v", err)
	}

	read := func() (status string, missing int, closedAt *time.Time) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			`SELECT status::text, missing_count, closed_at FROM job_postings WHERE id = $1`,
			postingID).Scan(&status, &missing, &closedAt); err != nil {
			t.Fatalf("read posting: %v", err)
		}
		return status, missing, closedAt
	}

	reconcile := func(seen []string) int64 {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		n, err := ReconcileAbsent(ctx, tx, sourceID, seen)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
		return n
	}

	// Still listed: nothing moves, however many times we poll.
	reconcile([]string{"ext-Backend Engineer"})
	if status, missing, _ := read(); status != "live" || missing != 0 {
		t.Fatalf("a listed posting changed: status=%s missing=%d", status, missing)
	}

	// Gone once. Counted, but a single absence must not close it — boards
	// reorder under pagination and a posting can flicker.
	if n := reconcile(nil); n != 0 {
		t.Errorf("closed %d postings after one absence, want 0", n)
	}
	if status, missing, closedAt := read(); status != "live" || missing != 1 || closedAt != nil {
		t.Fatalf("after one absence: status=%s missing=%d closed_at=%v", status, missing, closedAt)
	}

	// Gone twice: closed, with the timestamp set.
	if n := reconcile(nil); n != 1 {
		t.Errorf("closed %d postings after two absences, want 1", n)
	}
	status, missing, closedAt := read()
	if status != "closed" {
		t.Errorf("status = %s, want closed", status)
	}
	if closedAt == nil {
		t.Error("closed_at is nil on a closed posting — the freshness signal and " +
			"the source_daily rollup both read it")
	}
	if missing != 2 {
		t.Errorf("missing_count = %d, want 2", missing)
	}

	// A closed posting is not counted again: the WHERE clause is status='live'.
	if n := reconcile(nil); n != 0 {
		t.Errorf("closed %d already-closed postings, want 0", n)
	}
}

// An empty body must never erase one we already hold.
//
// This is the guard for the bug that cost 36% of the corpus its description.
// Two-phase vendors serve bodies from a per-posting endpoint and fill a bounded
// window per poll, but every poll re-upserts the WHOLE board — so before the
// fix, each poll wrote a real body for the window and blanked the thousands
// outside it. Coverage could never accumulate; it settled at window ÷ board
// size, which measured 1.9% on a 3,995-posting board.
//
// The test writes a body, then upserts the same posting without one, exactly as
// a poll outside the detail window does.
func TestUpsertPosting_AnEmptyBodyDoesNotEraseAStoredOne(t *testing.T) {
	pool := newTestDB(t)
	ctx := context.Background()

	postingID := seedPosting(t, pool, "Platform Engineer")
	var sourceID, companyID int64
	if err := pool.QueryRow(ctx,
		`SELECT source_id, company_id FROM job_postings WHERE id = $1`, postingID).
		Scan(&sourceID, &companyID); err != nil {
		t.Fatalf("read ids: %v", err)
	}

	upsert := func(html, text string, conf float64) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = UpsertPosting(ctx, tx, Posting{
			SourceID: sourceID, CompanyID: companyID,
			ExternalID: "ext-Platform Engineer", Title: "Platform Engineer",
			TitleNormalised: "platform engineer",
			DescriptionHTML: html, DescriptionText: text,
			ApplyURL: "https://example.test/apply", ParseConfidence: conf,
			// mode is a NOT NULL enum with no empty member, so the zero value of
			// the struct is not a valid row. Real ingest always sets it via
			// normalise.Mode.
			Mode:  "unknown",
			Field: "software", FieldConfidence: 0.9, FieldBecause: "test",
		})
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	read := func() (html, text string, conf float64) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			`SELECT description_html, description_text, parse_confidence
			   FROM job_postings WHERE id = $1`, postingID).Scan(&html, &text, &conf); err != nil {
			t.Fatalf("read posting: %v", err)
		}
		return html, text, conf
	}

	body := "<p>" + strings.Repeat("we build platforms. ", 40) + "</p>"
	upsert(body, strings.Repeat("we build platforms. ", 40), 0.9)
	if html, _, _ := read(); html == "" {
		t.Fatal("the detail phase's body was not stored at all")
	}

	// The next poll, for a posting outside the detail window: same row, no body.
	upsert("", "", 0.2)

	html, text, conf := read()
	if html == "" || text == "" {
		t.Error("an empty body erased the stored one — the corpus cannot accumulate")
	}
	// parse_confidence is derived FROM the body, so it has to follow it. Keeping
	// the description while taking the confidence computed for an absence would
	// leave the row readable and permanently below the scoring floor.
	// Compared with a tolerance: parse_confidence is a float4, so 0.9 round-trips
	// as 0.89999997 and an exact test fails on the storage type rather than on
	// the behaviour.
	if conf < 0.89 {
		t.Errorf("parse_confidence fell to %.4f with the body intact", conf)
	}

	// A real body still replaces a real body: this must not become a write-once
	// column, or an edited posting freezes at its first version.
	upsert("<p>rewritten</p>", "rewritten", 0.8)
	if _, text, _ := read(); text != "rewritten" {
		t.Errorf("a genuine update was rejected: text = %q", text)
	}
}
