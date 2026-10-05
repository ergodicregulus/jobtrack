//go:build integration

package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/datamigrations"
)

// Dedup had no test, and hid 4,310 postings still on their boards for it: it
// compared postings inside one source, on employer free text and seniority-blind
// titles.

type dedupeFixture struct {
	pool      *pgxpool.Pool
	companyID int64
	sources   map[string]int64
}

func newDedupeFixture(t *testing.T) *dedupeFixture {
	t.Helper()
	ctx := context.Background()
	f := &dedupeFixture{pool: newTestDB(t), sources: map[string]int64{}}
	if err := f.pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug) VALUES ('Acme', 'acme') RETURNING id`).Scan(&f.companyID); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"greenhouse", "ashby"} {
		var id int64
		if err := f.pool.QueryRow(ctx,
			`INSERT INTO sources (company_id, vendor, board_token) VALUES ($1, $2, 'acme') RETURNING id`,
			f.companyID, v).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.sources[v] = id
	}
	return f
}

// posting inserts a live posting. A longer description makes it the better
// record, which decides the keeper.
func (f *dedupeFixture) posting(t *testing.T, vendor, ext, title, city, req, desc string) int64 {
	t.Helper()
	var id int64
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO job_postings (company_id, source_id, external_id, title, title_normalised,
		                          city, requisition_id, description_text, status, apply_url, posted_at)
		VALUES ($1, $2, $3, $4, lower($4), NULLIF($5,''), NULLIF($6,''), $7, 'live',
		        'https://example.test/' || $3, now())
		RETURNING id`,
		f.companyID, f.sources[vendor], ext, title, city, req, desc).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *dedupeFixture) status(t *testing.T, id int64) string {
	t.Helper()
	var s string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status::text FROM job_postings WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDedupe_NeverMergesInsideASource(t *testing.T) {
	f := newDedupeFixture(t)

	// Each pair is what the old rules merged, from the live corpus.
	ids := []int64{
		// Stripe: one placeholder requisition on every posting.
		f.posting(t, "greenhouse", "1", "Backend Engineer, Payments", "Dublin", "See Opening ID", "long body"),
		f.posting(t, "greenhouse", "2", "Account Executive", "Dublin", "See Opening ID", "x"),
		// Seniority the normalised title drops.
		f.posting(t, "greenhouse", "3", "Senior Software Engineer", "", "", "long body"),
		f.posting(t, "greenhouse", "4", "Principal Software Engineer", "", "", "x"),
		// An identical title twice on one board is still two postings.
		f.posting(t, "greenhouse", "5", "Product Engineer", "London", "", "long body"),
		f.posting(t, "greenhouse", "6", "Product Engineer", "London", "", "x"),
	}

	if _, err := DedupeAcrossSources(context.Background(), f.pool, f.companyID); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if s := f.status(t, id); s != "live" {
			t.Errorf("posting %d is %s; a source's own postings are never duplicates of each other", id, s)
		}
	}
}

func TestDedupe_MergesOneRoleCarriedByTwoSources(t *testing.T) {
	f := newDedupeFixture(t)

	keeper := f.posting(t, "greenhouse", "1", "Senior Backend Engineer", "Berlin", "", "a much longer description")
	copyOf := f.posting(t, "ashby", "a1", "Senior Backend Engineer", "Berlin", "", "short")
	otherCity := f.posting(t, "ashby", "a2", "Senior Backend Engineer", "Lisbon", "", "short")
	otherLevel := f.posting(t, "ashby", "a3", "Staff Backend Engineer", "Berlin", "", "short")

	n, err := DedupeAcrossSources(context.Background(), f.pool, f.companyID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || f.status(t, copyOf) != "superseded" || f.status(t, keeper) != "live" {
		t.Fatalf("superseded %d; copy is %s, keeper %s — want the one copy merged into the fuller record",
			n, f.status(t, copyOf), f.status(t, keeper))
	}
	if f.status(t, otherCity) != "live" || f.status(t, otherLevel) != "live" {
		t.Errorf("another city (%s) or another level (%s) was merged; both are different jobs",
			f.status(t, otherCity), f.status(t, otherLevel))
	}
}

// The repair puts each wrongly merged row back to what its board says, and
// leaves a genuine cross-source merge alone.
func TestUndoInSourceSupersession(t *testing.T) {
	ctx := context.Background()
	f := newDedupeFixture(t)

	keeper := f.posting(t, "greenhouse", "1", "Backend Engineer", "", "See Opening ID", "long")
	onBoard := f.posting(t, "greenhouse", "2", "Account Executive", "", "See Opening ID", "x")
	gone := f.posting(t, "greenhouse", "3", "Designer", "", "See Opening ID", "x")
	crossKeeper := f.posting(t, "greenhouse", "4", "Data Engineer", "", "", "long")
	crossCopy := f.posting(t, "ashby", "a1", "Data Engineer", "", "", "x")

	// The state the old dedup left: the source's last full poll at T, the board
	// still listing onBoard at T and gone last seen a day before it.
	if _, err := f.pool.Exec(ctx, `
		UPDATE sources SET last_changed_at = now() WHERE id = $1;`, f.sources["greenhouse"]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE job_postings SET last_seen_at = (SELECT last_changed_at FROM sources WHERE id = $1)
		 WHERE source_id = $1`, f.sources["greenhouse"]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE job_postings SET last_seen_at = last_seen_at - interval '1 day' WHERE id = $1`, gone); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE job_postings SET status = 'superseded', canonical_id = $1 WHERE id = ANY($2);
	`, keeper, []int64{onBoard, gone}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE job_postings SET status = 'superseded', canonical_id = $1 WHERE id = $2`,
		crossKeeper, crossCopy); err != nil {
		t.Fatal(err)
	}

	m := &datamigrations.UndoInSourceSupersession{}
	if n, err := m.EstimateTotal(ctx, f.pool); err != nil || n != 2 {
		t.Fatalf("estimate %d, %v; want the 2 in-source merges", n, err)
	}
	var cursor []byte
	for i := 0; ; i++ {
		next, _, done, err := m.Batch(ctx, f.pool, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if i > 3 {
			t.Fatal("the migration did not finish")
		}
		cursor = next
	}

	for id, want := range map[int64]string{
		onBoard: "live", gone: "closed", keeper: "live", crossCopy: "superseded",
	} {
		if got := f.status(t, id); got != want {
			t.Errorf("posting %d is %s, want %s", id, got, want)
		}
	}
	var canonical *int64
	if err := f.pool.QueryRow(ctx, `SELECT canonical_id FROM job_postings WHERE id = $1`, onBoard).Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	if canonical != nil {
		t.Errorf("a restored posting still points at canonical %d", *canonical)
	}
}
