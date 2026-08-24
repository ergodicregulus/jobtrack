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

	"github.com/jobtrack/jobtrack/internal/migrate"
	"github.com/jobtrack/jobtrack/migrations"
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
