//go:build integration

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/seed"
	"github.com/ergodicregulus/jobtrack/internal/source"
)

// EnsureBoards now runs on every ingestor start, in production, and its prune
// deletes postings. Every run of it before this test reported pruned=0, so the
// delete path had never been exercised at all.
func TestEnsureBoards_RegistersIdempotentlyAndPrunes(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	boards := []seed.Board{
		{Slug: "acme", Name: "Acme", Domain: "acme.example", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "acme"},
		{Slug: "globex", Name: "Globex", Domain: "globex.example", Country: "IN",
			Vendor: source.VendorAshby, Token: "globex"},
	}

	reg, pruned, err := EnsureBoards(ctx, pool, boards)
	if err != nil || reg != 2 || pruned != 0 {
		t.Fatalf("first run: registered=%d pruned=%d err=%v, want 2, 0, nil", reg, pruned, err)
	}

	// Polling state must survive a restart: a board mid-sweep keeps its tier.
	if _, err := pool.Exec(ctx, `UPDATE sources SET tier = 'c' WHERE board_token = 'acme'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureBoards(ctx, pool, boards); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var tier string
	var sources int
	if err := pool.QueryRow(ctx, `SELECT tier::text FROM sources WHERE board_token = 'acme'`).Scan(&tier); err != nil {
		t.Fatal(err)
	}
	if tier != "c" {
		t.Errorf("a restart reset acme's tier to %q; it must leave polling state alone", tier)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sources`).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if sources != 2 {
		t.Errorf("a second run left %d sources, want 2 — it must be idempotent", sources)
	}

	// Globex leaves the list. Its source, its postings and its company go.
	if _, err := pool.Exec(ctx, `
		INSERT INTO job_postings
			(source_id, company_id, external_id, title, title_normalised, status, apply_url, posted_at)
		SELECT s.id, s.company_id, 'g-1', 'Engineer', 'engineer', 'live',
		       'https://globex.example/apply', now()
		  FROM sources s WHERE s.board_token = 'globex'`); err != nil {
		t.Fatalf("seed a posting: %v", err)
	}
	_, pruned, err = EnsureBoards(ctx, pool, boards[:1])
	if err != nil {
		t.Fatalf("pruning run: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned %d sources, want 1", pruned)
	}
	var postings, companies int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job_postings`).Scan(&postings); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM companies WHERE slug = 'globex'`).Scan(&companies); err != nil {
		t.Fatal(err)
	}
	if postings != 0 || companies != 0 {
		t.Errorf("after pruning globex: %d postings, %d globex companies left, want 0 and 0", postings, companies)
	}
}

// Against an empty list the prune would delete every posting there is, and this
// runs on every deploy. It must refuse rather than obey.
func TestEnsureBoards_RefusesAnEmptyList(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	if _, _, err := EnsureBoards(ctx, pool, []seed.Board{
		{Slug: "acme", Name: "Acme", Domain: "acme.example", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "acme"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureBoards(ctx, pool, nil); !errors.Is(err, ErrNoBoards) {
		t.Fatalf("an empty list returned %v, want ErrNoBoards", err)
	}
	var sources int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sources`).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if sources != 1 {
		t.Errorf("an empty list left %d sources, want the 1 that was there", sources)
	}
}
