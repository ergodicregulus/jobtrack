//go:build integration

package store

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// TestLoadDashboardEmpty is the cheapest possible guard against the failure
// ADR-0015 accepted: every one of the seven batched statements must parse, and
// every column must map onto a struct field by name. A misspelled alias or a
// renamed field fails here rather than in production.
func TestLoadDashboardEmpty(t *testing.T) {
	pool := newTestDB(t)
	userID := seedUser(t, pool, "empty@example.test")

	d, err := LoadDashboard(context.Background(), pool, userID)
	if err != nil {
		t.Fatalf("LoadDashboard: %v", err)
	}

	if d.Matches.Strong != 0 || d.Matches.Scored != 0 {
		t.Errorf("new user should have no matches, got %+v", d.Matches)
	}
	if len(d.NeedsReply) != 0 || len(d.TopMatches) != 0 {
		t.Errorf("new user should have nothing to act on, got %d/%d", len(d.NeedsReply), len(d.TopMatches))
	}
	if d.Pipeline == nil {
		t.Error("pipeline counts should be an empty map, not nil")
	}
}

// TestLoadDashboardWithData exercises the scan path that actually carries
// values, including the array column and the EXISTS-derived boolean.
func TestLoadDashboardWithData(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	userID := seedUser(t, pool, "full@example.test")
	postingID := seedPosting(t, pool, "Backend Engineer")

	_, err := pool.Exec(ctx, `
		INSERT INTO user_job_scores
			(user_id, posting_id, score, band, missing_skills, components, confidence, profile_version)
		VALUES ($1, $2, 91.5, 'strong', ARRAY['kubernetes','terraform'], '{}'::jsonb, 1.0, 'test')`,
		userID, postingID)
	if err != nil {
		t.Fatalf("seed score: %v", err)
	}

	d, err := LoadDashboard(ctx, pool, userID)
	if err != nil {
		t.Fatalf("LoadDashboard: %v", err)
	}

	if d.Matches.Strong != 1 {
		t.Errorf("strong = %d, want 1", d.Matches.Strong)
	}
	if d.Matches.Scored != 1 {
		t.Errorf("scored = %d, want 1", d.Matches.Scored)
	}
	if len(d.TopMatches) != 1 {
		t.Fatalf("top matches = %d, want 1", len(d.TopMatches))
	}

	top := d.TopMatches[0]
	if top.Title != "Backend Engineer" {
		t.Errorf("title = %q", top.Title)
	}
	if top.Score != 91.5 || top.Band != "strong" {
		t.Errorf("score/band = %v/%q, want 91.5/strong", top.Score, top.Band)
	}
	// The array column and the EXISTS boolean are the two columns most likely
	// to be silently mis-scanned, so they are asserted explicitly.
	if len(top.Missing) != 2 || top.Missing[0] != "kubernetes" {
		t.Errorf("missing skills = %v, want [kubernetes terraform]", top.Missing)
	}
	if top.Saved {
		t.Error("posting has no application, so saved should be false")
	}
	if top.PostedAt.IsZero() {
		t.Error("posted_at did not scan")
	}
	if d.Market.LivePostings != 1 || d.Market.Companies != 1 {
		t.Errorf("market = %+v, want 1 posting / 1 company", d.Market)
	}
}

// TestApplicationLifecycle walks save -> advance -> event, which is the path
// that writes application_events. That table went unwritten for months while
// looking like a working feature, so the assertion is on the event, not just
// on the status.
func TestApplicationLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	userID := seedUser(t, pool, "track@example.test")
	postingID := seedPosting(t, pool, "Platform Engineer")

	saved, err := SaveJob(ctx, pool, userID, postingID)
	if err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	if saved.Status != "saved" || saved.RoleTitle != "Platform Engineer" {
		t.Fatalf("saved = %+v", saved)
	}

	// Saving twice must be the same as saving once.
	again, err := SaveJob(ctx, pool, userID, postingID)
	if err != nil {
		t.Fatalf("SaveJob (repeat): %v", err)
	}
	if again.ID != saved.ID {
		t.Errorf("second save created a new row: %d != %d", again.ID, saved.ID)
	}

	applied := "applied"
	if err := UpdateApplication(ctx, pool, userID, saved.ID, ApplicationPatch{Status: &applied}); err != nil {
		t.Fatalf("UpdateApplication: %v", err)
	}

	var events int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM application_events WHERE application_id = $1`, saved.ID).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Errorf("transition saved->applied recorded %d events, want 1", events)
	}

	// A no-op patch must not manufacture an event.
	note := "still waiting"
	if err := UpdateApplication(ctx, pool, userID, saved.ID, ApplicationPatch{Note: &note}); err != nil {
		t.Fatalf("UpdateApplication (note only): %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM application_events WHERE application_id = $1`, saved.ID).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Errorf("editing a note recorded an event; events = %d, want 1", events)
	}

	// Advanced applications must not be deletable.
	if err := UnsaveJob(ctx, pool, userID, postingID); err != ErrNotRemovable {
		t.Errorf("UnsaveJob on an applied row = %v, want ErrNotRemovable", err)
	}

	items, err := ListApplications(ctx, pool, userID)
	if err != nil {
		t.Fatalf("ListApplications: %v", err)
	}
	if len(items) != 1 || items[0].Status != "applied" {
		t.Errorf("list = %+v", items)
	}
}

// TestSaveJobUnknownPosting must report absence, not a database error.
func TestSaveJobUnknownPosting(t *testing.T) {
	pool := newTestDB(t)
	userID := seedUser(t, pool, "missing@example.test")

	if _, err := SaveJob(context.Background(), pool, userID, 999999); err != ErrNotFound {
		t.Errorf("SaveJob on a missing posting = %v, want ErrNotFound", err)
	}
}

// TestLoadActivity checks the window bounds and the day/count mapping.
func TestLoadActivity(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	userID := seedUser(t, pool, "activity@example.test")
	postingID := seedPosting(t, pool, "SRE")
	saved, err := SaveJob(ctx, pool, userID, postingID)
	if err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	applied := "applied"
	if err := UpdateApplication(ctx, pool, userID, saved.ID, ApplicationPatch{Status: &applied}); err != nil {
		t.Fatalf("UpdateApplication: %v", err)
	}

	w, err := LoadActivity(ctx, pool, userID, 12)
	if err != nil {
		t.Fatalf("LoadActivity: %v", err)
	}
	if w.Total != 1 {
		t.Errorf("total = %d, want 1", w.Total)
	}
	if len(w.Days) != 1 || w.Days[0].Count != 1 {
		t.Errorf("days = %+v, want one day with count 1", w.Days)
	}
	if w.From == "" || w.To == "" || w.From >= w.To {
		t.Errorf("window bounds are wrong: %q .. %q", w.From, w.To)
	}
}

// TestLoadPostingDetail covers the widest projection in the codebase: 32
// columns, two correlated array subqueries, two LEFT JOINs and a derived
// boolean. It is the query most likely to be silently mis-scanned, so it is
// asserted column by column rather than "no error".
func TestLoadPostingDetail(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	userID := seedUser(t, pool, "detail@example.test")
	postingID := seedPosting(t, pool, "Staff Engineer")

	// Anonymous viewer: both LEFT JOINs miss, so the score fields stay nil.
	anon, err := LoadPostingDetail(ctx, pool, postingID, nil)
	if err != nil {
		t.Fatalf("LoadPostingDetail(anonymous): %v", err)
	}
	if anon.Title != "Staff Engineer" || anon.CompanySlug == "" || anon.Vendor != "greenhouse" {
		t.Errorf("anonymous row = %+v", anon)
	}
	if anon.Score != nil || anon.Band != nil {
		t.Error("anonymous viewer must have no score")
	}
	if anon.Saved {
		t.Error("anonymous viewer must not appear to have saved anything")
	}
	if anon.MustHaveSkills == nil || anon.NiceToHaveSkills == nil {
		t.Error("skill arrays should be empty slices, not nil")
	}

	// Signed-in viewer with a score and a saved application.
	_, err = pool.Exec(ctx, `
		INSERT INTO user_job_scores
			(user_id, posting_id, score, band, missing_skills, components, confidence, profile_version)
		VALUES ($1, $2, 77.25, 'plausible', ARRAY['rust'], '[{"name":"skills"}]'::jsonb, 0.8, 'test')`,
		userID, postingID)
	if err != nil {
		t.Fatalf("seed score: %v", err)
	}
	if _, err := SaveJob(ctx, pool, userID, postingID); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	seen, err := LoadPostingDetail(ctx, pool, postingID, userID)
	if err != nil {
		t.Fatalf("LoadPostingDetail(viewer): %v", err)
	}
	if seen.Score == nil || *seen.Score != 77.25 {
		t.Errorf("score = %v, want 77.25", seen.Score)
	}
	if seen.Band == nil || *seen.Band != "plausible" {
		t.Errorf("band = %v, want plausible", seen.Band)
	}
	// confidence is Postgres `real`, so 0.8 does not survive float32 exactly.
	// Comparing widened float32 values for equality is the bug this avoids.
	if seen.Confidence == nil || math.Abs(*seen.Confidence-0.8) > 1e-6 {
		t.Errorf("confidence = %v, want ~0.8", deref(seen.Confidence))
	}
	if len(seen.Components) == 0 {
		t.Error("components JSON did not scan")
	}
	if len(seen.Missing) != 1 || seen.Missing[0] != "rust" {
		t.Errorf("missing = %v, want [rust]", seen.Missing)
	}
	if !seen.Saved {
		t.Error("saved should be true once an application exists")
	}
	if seen.ComputedAt == nil {
		t.Error("computed_at did not scan")
	}

	// A closed posting is reported absent, not as an empty row.
	if _, err := pool.Exec(ctx,
		`UPDATE job_postings SET status = 'closed' WHERE id = $1`, postingID); err != nil {
		t.Fatalf("close posting: %v", err)
	}
	if _, err := LoadPostingDetail(ctx, pool, postingID, userID); err != ErrNotFound {
		t.Errorf("closed posting = %v, want ErrNotFound", err)
	}
}

// deref prints a pointer's value, so a failure message says what was wrong
// rather than where it was stored.
func deref[T any](p *T) any {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// TestFeedCursorPagination is a regression test for a bug that made "load more"
// return 500 for every sort mode.
//
// The keyset comparison cast the sort COLUMN to text while the ORDER BY used
// its native type, so the predicate read `timestamptz < text` — which Postgres
// rejects. For the numeric sorts it would have been worse if it had compiled:
// text comparison orders lexicographically, so a salary of 9 sorts after 100.
//
// Every sort mode is exercised because the bug was in code shared by all three.
func TestFeedCursorPagination(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	userID := seedUser(t, pool, "feed@example.test")
	for i := range 5 {
		seedPosting(t, pool, fmt.Sprintf("Engineer %d", i))
	}

	for _, mode := range []string{"newest", "comp", "match"} {
		t.Run(mode, func(t *testing.T) {
			first, err := Feed(ctx, pool, FeedFilter{UserID: &userID, Limit: 2, Sort: mode}, "")
			if err != nil {
				t.Fatalf("first page: %v", err)
			}
			if len(first.Items) != 2 {
				t.Fatalf("first page has %d items, want 2", len(first.Items))
			}
			if !first.HasMore || first.NextCursor == "" {
				t.Fatalf("first page should report more, got HasMore=%v cursor=%q",
					first.HasMore, first.NextCursor)
			}

			second, err := Feed(ctx, pool, FeedFilter{UserID: &userID, Limit: 2, Sort: mode}, first.NextCursor)
			if err != nil {
				t.Fatalf("second page: %v", err)
			}
			if len(second.Items) == 0 {
				t.Fatal("second page is empty")
			}

			// The pages must not overlap, which is the property keyset
			// pagination exists to provide.
			seen := map[int64]bool{}
			for _, it := range first.Items {
				seen[it.ID] = true
			}
			for _, it := range second.Items {
				if seen[it.ID] {
					t.Errorf("posting %d appears on both pages", it.ID)
				}
			}
		})
	}
}

// TestReadsSurviveAllNullableColumns is a guard for a whole bug class, not one
// bug. Three of them were found by hand on 2026-08-25: yoe_confidence in the
// posting detail, then yoe_confidence and location_raw again in the feed. Each
// was a nullable column scanned into a plain Go value, so a single NULL would
// have failed the scan and taken the whole endpoint with it.
//
// None of them could fire against production data, because UpsertPosting always
// writes those columns. That is exactly why they survived: the only way to
// reach them is a row inserted by some other path — a data migration, a new
// adapter, a manual fix — which is a thing that happens rarely and at the worst
// possible time.
//
// So this inserts the most hostile row the schema permits: every NOT NULL
// column set, every nullable column left NULL. Any read path that cannot
// tolerate it fails here instead of in production.
func TestReadsSurviveAllNullableColumns(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	userID := seedUser(t, pool, "nulls@example.test")

	var companyID, sourceID, postingID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug) VALUES ('Null Co', 'null-co') RETURNING id`).
		Scan(&companyID); err != nil {
		t.Fatalf("seed company: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO sources (company_id, vendor, board_token) VALUES ($1, 'greenhouse', 'nulls')
		 RETURNING id`, companyID).Scan(&sourceID); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	// Only the columns the schema actually requires. Everything nullable stays
	// NULL, including location_raw and yoe_confidence.
	if err := pool.QueryRow(ctx, `
		INSERT INTO job_postings
			(company_id, source_id, external_id, title, title_normalised, status, apply_url)
		VALUES ($1, $2, 'null-ext', 'Role With Nothing Stated', 'role with nothing stated',
		        'live', 'https://example.test/apply')
		RETURNING id`, companyID, sourceID).Scan(&postingID); err != nil {
		t.Fatalf("seed posting: %v", err)
	}

	// Assert the row really is as sparse as intended, so this test cannot
	// quietly stop testing anything if a future migration adds a default.
	var nulls int
	if err := pool.QueryRow(ctx, `
		SELECT (location_raw IS NULL)::int + (yoe_confidence IS NULL)::int
		     + (posted_at IS NULL)::int + (comp_min IS NULL)::int
		  FROM job_postings WHERE id = $1`, postingID).Scan(&nulls); err != nil {
		t.Fatalf("check sparseness: %v", err)
	}
	if nulls != 4 {
		t.Fatalf("the posting is not sparse enough to test anything: %d/4 columns NULL", nulls)
	}

	t.Run("feed", func(t *testing.T) {
		page, err := Feed(ctx, pool, FeedFilter{UserID: &userID, Limit: 10}, "")
		if err != nil {
			t.Fatalf("Feed: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("feed returned %d items, want 1", len(page.Items))
		}
		if page.Items[0].Title != "Role With Nothing Stated" {
			t.Errorf("title = %q", page.Items[0].Title)
		}
	})

	t.Run("posting detail, anonymous", func(t *testing.T) {
		if _, err := LoadPostingDetail(ctx, pool, postingID, nil); err != nil {
			t.Fatalf("LoadPostingDetail: %v", err)
		}
	})

	t.Run("posting detail, signed in", func(t *testing.T) {
		if _, err := LoadPostingDetail(ctx, pool, postingID, userID); err != nil {
			t.Fatalf("LoadPostingDetail: %v", err)
		}
	})

	t.Run("dashboard", func(t *testing.T) {
		if _, err := LoadDashboard(ctx, pool, userID); err != nil {
			t.Fatalf("LoadDashboard: %v", err)
		}
	})

	t.Run("save and list", func(t *testing.T) {
		if _, err := SaveJob(ctx, pool, userID, postingID); err != nil {
			t.Fatalf("SaveJob: %v", err)
		}
		if _, err := ListApplications(ctx, pool, userID); err != nil {
			t.Fatalf("ListApplications: %v", err)
		}
	})

	t.Run("scoring reads", func(t *testing.T) {
		if _, err := PostingForScoring(ctx, pool, postingID); err != nil {
			t.Fatalf("PostingForScoring: %v", err)
		}
		if _, err := PostingsForScoring(ctx, pool, 10); err != nil {
			t.Fatalf("PostingsForScoring: %v", err)
		}
	})
}
