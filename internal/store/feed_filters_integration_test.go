//go:build integration

package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Filter correctness, asserted in BOTH directions.
//
// A filter can fail two ways and only one of them is visible. Returning
// something that does not match is a false positive: the reader sees it and can
// tell. Omitting something that does match is a false negative: the reader sees
// nothing and cannot tell — which is why "no misses" is the harder half and why
// every case here asserts the EXACT set of ids, not a count and not a spot check.
//
// The corpus is built here rather than sampled from the live database, so the
// right answer is known by construction. A test that asks the database what the
// answer is and then checks the database agrees proves only that one query ran
// twice.

// fixture is one posting with everything a filter reads.
type fixture struct {
	ext      string
	title    string
	country  string // "" means NULL
	mode     string
	yoeMin   *int
	compMax  *float64
	compMin  *float64
	currency string
	skills   []string
	field    string
	postedAt time.Time
	vendor   string
	body     string
}

// cur defaults the currency so a fixture that does not care about money still
// writes a valid row.
func cur(v string) string {
	if v == "" {
		return "USD"
	}
	return v
}

func seedFilterCorpus(t *testing.T, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	i := func(v int) *int { return &v }
	f := func(v float64) *float64 { return &v }

	corpus := []fixture{
		{ext: "in-remote", title: "Backend Engineer", country: "IN", mode: "remote",
			yoeMin: i(3), compMax: f(4000000), compMin: f(3500000), currency: "INR",
			skills: []string{"go", "postgresql"},
			field:  "software", postedAt: now.AddDate(0, 0, -1),
			vendor: "greenhouse", body: "we use go and postgres"},
		{ext: "us-remote", title: "Platform Engineer", country: "US", mode: "remote",
			yoeMin: i(6), compMax: f(220000), compMin: f(180000), currency: "USD",
			skills: []string{"kubernetes", "terraform"},
			field:  "software", postedAt: now.AddDate(0, 0, -3),
			vendor: "ashby", body: "kubernetes and terraform"},
		{ext: "us-onsite", title: "Data Engineer", country: "US", mode: "onsite",
			yoeMin: i(1), compMax: f(120000), field: "software", postedAt: now.AddDate(0, 0, -10),
			vendor: "greenhouse", body: "python and airflow"},
		// £150,000 is about $203,000. Numerically below a 200000 bar and above it
		// once converted — the exact shape that was being hidden from a "$200k+"
		// search before the comparison was fixed.
		{ext: "gb-hybrid", title: "Frontend Engineer", country: "GB", mode: "hybrid",
			yoeMin: i(4), compMax: f(190000), compMin: f(150000), currency: "GBP",
			skills: []string{"typescript"},
			field:  "software", postedAt: now.AddDate(0, 0, -20),
			vendor: "ashby", body: "typescript and svelte"},
		// No country: must never be hidden by a country filter.
		{ext: "nowhere", title: "Site Reliability Engineer", country: "", mode: "remote",
			yoeMin: i(5), compMax: f(180000), field: "software", postedAt: now.AddDate(0, 0, -2),
			vendor: "greenhouse", body: "linux and observability"},
		// No stated experience: must never be hidden by an experience filter.
		{ext: "no-yoe", title: "Software Engineer", country: "IN", mode: "hybrid",
			yoeMin: nil, compMax: f(3000000), field: "software", postedAt: now.AddDate(0, 0, -5),
			vendor: "greenhouse", body: "java and spring"},
		// No salary: must never be hidden unless the reader asks for disclosed only.
		{ext: "no-comp", title: "Mobile Engineer", country: "US", mode: "remote",
			yoeMin: i(2), compMax: nil, field: "software", postedAt: now.AddDate(0, 0, -4),
			vendor: "ashby", body: "swift and kotlin"},
		// Not software: hidden by default, visible under field=all.
		{ext: "sales", title: "Account Executive", country: "US", mode: "onsite",
			yoeMin: i(3), compMax: f(150000), field: "other", postedAt: now.AddDate(0, 0, -6),
			vendor: "greenhouse", body: "quota and pipeline"},
		// Old, for the freshness window.
		{ext: "ancient", title: "Systems Engineer", country: "IN", mode: "remote",
			yoeMin: i(8), compMax: f(5000000), field: "software", postedAt: now.AddDate(0, 0, -120),
			vendor: "ashby", body: "c and embedded"},
	}

	ids := map[string]int64{}
	var companyID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug) VALUES ('FilterCo','filterco') RETURNING id`).
		Scan(&companyID); err != nil {
		t.Fatalf("seed company: %v", err)
	}

	sources := map[string]int64{}
	for _, want := range []string{"greenhouse", "ashby"} {
		var id int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO sources (company_id, vendor, board_token)
			 VALUES ($1, $2::source_vendor, $3) RETURNING id`,
			companyID, want, "tok-"+want).Scan(&id); err != nil {
			t.Fatalf("seed source %s: %v", want, err)
		}
		sources[want] = id
	}

	for _, c := range corpus {
		var id int64
		var country any
		if c.country != "" {
			country = c.country
		}
		err := pool.QueryRow(ctx, `
			INSERT INTO job_postings
				(company_id, source_id, external_id, title, title_normalised,
				 description_text, description_html, status, apply_url,
				 country, mode, yoe_min, comp_max, comp_currency, field, posted_at,
				 parse_confidence)
			VALUES ($1,$2,$3,$4,lower($4),$5,$5,'live','https://x.test/a',
			        $6,$7::work_mode,$8,$9,$10,$11,$12,0.9)
			RETURNING id`,
			companyID, sources[c.vendor], c.ext, c.title, c.body,
			country, c.mode, c.yoeMin, c.compMax, cur(c.currency), c.field, c.postedAt).Scan(&id)
		if err != nil {
			t.Fatalf("seed posting %s: %v", c.ext, err)
		}
		if c.compMin != nil {
			if _, err := pool.Exec(ctx,
				`UPDATE job_postings SET comp_min = $2 WHERE id = $1`, id, *c.compMin); err != nil {
				t.Fatalf("set comp_min %s: %v", c.ext, err)
			}
		}
		for _, sk := range c.skills {
			var skillID int64
			if err := pool.QueryRow(ctx, `
				INSERT INTO skills (canonical, display_name, category)
				VALUES ($1, $2, 'language')
				ON CONFLICT (canonical) DO UPDATE SET display_name = EXCLUDED.display_name
				RETURNING id`, sk, sk).Scan(&skillID); err != nil {
				t.Fatalf("seed skill %s: %v", sk, err)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO posting_skills (posting_id, skill_id, requirement)
				VALUES ($1, $2, 'must_have') ON CONFLICT DO NOTHING`, id, skillID); err != nil {
				t.Fatalf("link skill %s: %v", sk, err)
			}
		}
		ids[c.ext] = id
	}
	return ids
}

// run executes the feed and returns the external ids it produced, sorted.
func run(t *testing.T, pool *pgxpool.Pool, f FeedFilter) []string {
	t.Helper()
	f.Limit = 50
	page, err := Feed(context.Background(), pool, nil, f, "")
	if err != nil {
		t.Fatalf("Feed(%+v): %v", f, err)
	}
	out := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		var ext string
		if err := pool.QueryRow(context.Background(),
			`SELECT external_id FROM job_postings WHERE id = $1`, it.ID).Scan(&ext); err != nil {
			t.Fatalf("read external_id: %v", err)
		}
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

func assertSet(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		missing, extra := diff(want, got), diff(got, want)
		t.Errorf("%s\n  got:     %v\n  want:    %v\n  MISSING: %v  (a job the reader will never see)\n  EXTRA:   %v",
			what, got, want, missing, extra)
	}
}

func diff(a, b []string) []string {
	in := map[string]bool{}
	for _, v := range b {
		in[v] = true
	}
	var out []string
	for _, v := range a {
		if !in[v] {
			out = append(out, v)
		}
	}
	return out
}

func TestFeedFilters_EachFilterReturnsExactlyTheRightPostings(t *testing.T) {
	pool := newTestDB(t)
	seedFilterCorpus(t, pool)

	// Everything, so the rest can be read as subsets of a known whole.
	all := []string{"ancient", "gb-hybrid", "in-remote", "no-comp", "no-yoe",
		"nowhere", "sales", "us-onsite", "us-remote"}
	assertSet(t, "no filter, field=all, any age",
		run(t, pool, FeedFilter{Field: "all", PostedWithin: 0}), all...)

	t.Run("country is multi-select", func(t *testing.T) {
		// The reader asked for two countries. Both must come back, and neither
		// may bring the third.
		assertSet(t, "country=IN,US",
			run(t, pool, FeedFilter{Field: "all", Countries: []string{"IN", "US"}}),
			"ancient", "in-remote", "no-comp", "no-yoe", "nowhere", "sales", "us-onsite", "us-remote")

		assertSet(t, "country=GB",
			run(t, pool, FeedFilter{Field: "all", Countries: []string{"GB"}}),
			"gb-hybrid", "nowhere")
	})

	t.Run("a location we could not read is never hidden", func(t *testing.T) {
		// "nowhere" has no country. It appears under every country filter,
		// deliberately: our parse failure must not cost the reader a job. The
		// rail discloses this rather than leaving it to be discovered.
		for _, c := range [][]string{{"IN"}, {"US"}, {"GB"}, {"IN", "GB"}} {
			got := run(t, pool, FeedFilter{Field: "all", Countries: c})
			if !contains(got, "nowhere") {
				t.Errorf("country=%v dropped the posting with no country", c)
			}
		}
	})

	t.Run("work style is multi-select", func(t *testing.T) {
		assertSet(t, "mode=remote,hybrid",
			run(t, pool, FeedFilter{Field: "all", Modes: []string{"remote", "hybrid"}}),
			"ancient", "gb-hybrid", "in-remote", "no-comp", "no-yoe", "nowhere", "us-remote")
	})

	t.Run("vendor is multi-select", func(t *testing.T) {
		assertSet(t, "vendor=ashby",
			run(t, pool, FeedFilter{Field: "all", Vendors: []string{"ashby"}}),
			"ancient", "gb-hybrid", "no-comp", "us-remote")
	})

	t.Run("field defaults to hiding other", func(t *testing.T) {
		got := run(t, pool, FeedFilter{})
		if contains(got, "sales") {
			t.Error("the default feed showed a posting classified as other")
		}
		assertSet(t, "field=software", run(t, pool, FeedFilter{Field: "software"}),
			"ancient", "gb-hybrid", "in-remote", "no-comp", "no-yoe", "nowhere", "us-onsite", "us-remote")
	})

	// THE GAP THAT LET A REAL BUG THROUGH.
	//
	// The first version of this suite asserted exact sets for country, mode and
	// vendor, and for experience only checked that an unstated posting survives.
	// It never asked which postings an experience filter RETURNS — and the
	// filter was returning the wrong ones: a single threshold widened by an
	// invisible +2, so "0–2 yrs" came back with 3-, 4- and 3–5-year roles while
	// the chip beside it read 835.
	//
	// Asserting the exact set is the difference between a test that checks a
	// filter runs and one that checks it filters.
	t.Run("experience bands return exactly their band", func(t *testing.T) {
		// in-remote 3, us-remote 6, us-onsite 1, gb-hybrid 4, nowhere 5,
		// no-comp 2, sales 3, ancient 8, no-yoe unstated.
		assertSet(t, "yoe=0-2",
			run(t, pool, FeedFilter{Field: "all", YoEBands: []string{"0-2"}}),
			"no-comp", "no-yoe", "us-onsite")

		assertSet(t, "yoe=3-5",
			run(t, pool, FeedFilter{Field: "all", YoEBands: []string{"3-5"}}),
			"gb-hybrid", "in-remote", "no-yoe", "nowhere", "sales")

		// Multi-select is a union of the bands, never a widening of one.
		assertSet(t, "yoe=0-2,3-5",
			run(t, pool, FeedFilter{Field: "all", YoEBands: []string{"0-2", "3-5"}}),
			"gb-hybrid", "in-remote", "no-comp", "no-yoe", "nowhere", "sales", "us-onsite")

		assertSet(t, "yoe=6-8",
			run(t, pool, FeedFilter{Field: "all", YoEBands: []string{"6-8"}}),
			"ancient", "no-yoe", "us-remote")
	})

	t.Run("search matches title and body", func(t *testing.T) {
		got := run(t, pool, FeedFilter{Field: "all", Query: "kubernetes"})
		if !contains(got, "us-remote") {
			t.Errorf("full-text search missed a body term: %v", got)
		}
	})

	// The salary bar is a DOLLAR figure — the chips say "$100k" — so it has to
	// compare converted value. Comparing the raw number let a 3,575,300 INR role
	// worth $37,555 through a "$200k+" filter, and hid a GBP 150,000 role worth
	// $203,000 from it. The second is the expensive direction: a job the reader
	// never learns exists.
	t.Run("salary floor compares value, not the size of the number", func(t *testing.T) {
		got := run(t, pool, FeedFilter{Field: "all", CompMin: ptrF(200000), CompDisclosedOnly: true})
		// in-remote is 3,500,000 INR = about $36,800: numerically far above the
		// bar, actually far below it.
		if contains(got, "in-remote") {
			t.Error("an INR salary passed a $200k bar on its raw number")
		}
		// gb-hybrid is GBP 150,000 = about $203,000: numerically below, actually
		// above.
		if !contains(got, "gb-hybrid") {
			t.Error("a GBP salary worth $203k was hidden from a $200k bar")
		}
		assertSet(t, "comp_min=200000 disclosed only", got, "gb-hybrid")
	})

	t.Run("disclosed-only excludes exactly the undisclosed", func(t *testing.T) {
		assertSet(t, "comp_disclosed_only",
			run(t, pool, FeedFilter{Field: "all", CompDisclosedOnly: true}),
			"gb-hybrid", "in-remote", "us-remote")
	})

	t.Run("skills require ALL of them, never any", func(t *testing.T) {
		assertSet(t, "skills=kubernetes",
			run(t, pool, FeedFilter{Field: "all", Skills: []string{"kubernetes"}}), "us-remote")

		assertSet(t, "skills=kubernetes,terraform",
			run(t, pool, FeedFilter{Field: "all", Skills: []string{"kubernetes", "terraform"}}),
			"us-remote")

		// One posting has go, another has kubernetes; nothing has both. A filter
		// returning either would be an OR wearing an AND's label.
		assertSet(t, "skills=go,kubernetes",
			run(t, pool, FeedFilter{Field: "all", Skills: []string{"go", "kubernetes"}}))
	})

	t.Run("freshness window cuts exactly at its edge", func(t *testing.T) {
		// Ages: in-remote 1d, nowhere 2d, us-remote 3d, no-comp 4d, no-yoe 5d,
		// sales 6d, us-onsite 10d, gb-hybrid 20d, ancient 120d.
		assertSet(t, "posted_within=7d",
			run(t, pool, FeedFilter{Field: "all", PostedWithin: 7 * 24 * time.Hour}),
			"in-remote", "no-comp", "no-yoe", "nowhere", "sales", "us-remote")

		assertSet(t, "posted_within=14d",
			run(t, pool, FeedFilter{Field: "all", PostedWithin: 14 * 24 * time.Hour}),
			"in-remote", "no-comp", "no-yoe", "nowhere", "sales", "us-onsite", "us-remote")
	})
}

func contains(hay []string, needle string) bool {
	for _, v := range hay {
		if v == needle {
			return true
		}
	}
	return false
}

// An unstated value must never be filtered OUT. This is the "no misses" half,
// and it is the one a reader cannot detect for themselves.
func TestFeedFilters_UnstatedValuesAreNeverHidden(t *testing.T) {
	pool := newTestDB(t)
	seedFilterCorpus(t, pool)

	cases := []struct {
		what string
		f    FeedFilter
		keep string
	}{
		{"experience filter keeps a posting that states none",
			FeedFilter{Field: "all", YoEBands: []string{"3-5"}}, "no-yoe"},
		{"salary filter keeps a posting that publishes none",
			FeedFilter{Field: "all", CompMin: ptrF(100000)}, "no-comp"},
		{"country filter keeps a posting with no country",
			FeedFilter{Field: "all", Countries: []string{"IN"}}, "nowhere"},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			if got := run(t, pool, c.f); !contains(got, c.keep) {
				t.Errorf("%s: %q was filtered out\n  got: %v", c.what, c.keep, got)
			}
		})
	}

	// ...unless the reader explicitly asks to exclude them. That is a different
	// request and must be honoured exactly.
	got := run(t, pool, FeedFilter{Field: "all", CompDisclosedOnly: true})
	if contains(got, "no-comp") {
		t.Error("comp_disclosed_only still returned a posting with no salary")
	}
}

func ptrF(v float64) *float64 { return &v }

// Combining filters must intersect, never union. A reader who narrows twice and
// gets MORE results has been lied to about what a filter does.
func TestFeedFilters_CombiningNarrows(t *testing.T) {
	pool := newTestDB(t)
	seedFilterCorpus(t, pool)

	country := run(t, pool, FeedFilter{Field: "all", Countries: []string{"US"}})
	both := run(t, pool, FeedFilter{Field: "all",
		Countries: []string{"US"}, Modes: []string{"remote"}})

	if len(both) > len(country) {
		t.Fatalf("adding a filter widened the result: %d -> %d", len(country), len(both))
	}
	for _, ext := range both {
		if !contains(country, ext) {
			t.Errorf("%s appeared only when a second filter was added", ext)
		}
	}
	assertSet(t, "country=US AND mode=remote", both, "no-comp", "nowhere", "us-remote")
}

// Sorting by salary must compare VALUE, not the size of the number.
//
// The raw figure is wrong across currencies and visibly so: 13,750,000 JPY is
// about $92,000 and outranked every $850,000 role in the corpus, purely because
// yen are numerous. A reader sorting by salary got the currencies with the
// largest numbers.
func TestFeedSort_SalaryComparesValueNotMagnitude(t *testing.T) {
	pool := newTestDB(t)
	ctx := context.Background()

	var companyID, sourceID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug) VALUES ('FxCo','fxco') RETURNING id`).
		Scan(&companyID); err != nil {
		t.Fatalf("seed company: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO sources (company_id, vendor, board_token)
		 VALUES ($1,'greenhouse','fx') RETURNING id`, companyID).Scan(&sourceID); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	// Ordered by real value: 500k USD > 375k GBP (~508k... deliberately close)
	// > 13.75m JPY (~86k) > 4.99m INR (~52k).
	for _, c := range []struct {
		ext, cur string
		amount   float64
	}{
		{"usd-500k", "USD", 500000},
		{"jpy-13m", "JPY", 13750000},
		{"inr-5m", "INR", 4996800},
		{"gbp-300k", "GBP", 300000},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO job_postings
				(company_id, source_id, external_id, title, title_normalised, status,
				 apply_url, comp_min, comp_currency, field, posted_at, parse_confidence, mode)
			VALUES ($1,$2,$3,$4,lower($4),'live','https://x.test/a',$5,$6,'software',now(),0.9,'unknown')`,
			companyID, sourceID, c.ext, c.ext, c.amount, c.cur); err != nil {
			t.Fatalf("seed %s: %v", c.ext, err)
		}
	}

	page, err := Feed(ctx, pool, nil, FeedFilter{Field: "all", Sort: "comp", Limit: 10}, "")
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	var order []string
	for _, it := range page.Items {
		var ext string
		if err := pool.QueryRow(ctx, `SELECT external_id FROM job_postings WHERE id=$1`, it.ID).
			Scan(&ext); err != nil {
			t.Fatalf("read ext: %v", err)
		}
		order = append(order, ext)
	}

	want := []string{"usd-500k", "gbp-300k", "jpy-13m", "inr-5m"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("salary order compared magnitude, not value\n  got:  %v\n  want: %v", order, want)
	}
}

var _ = fmt.Sprintf
