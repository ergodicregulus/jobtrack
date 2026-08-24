// Command seed prepares a development database.
//
// It does NOT fabricate job postings. It registers real ATS boards and lets the
// ingestor fetch them, so every posting in development is a real posting with a
// working apply URL.
//
// The previous version generated synthetic postings with plausible-looking
// URLs. That was fine for exercising the pipeline offline, and wrong the moment
// anyone clicked one — a dead link in a product whose entire premise is
// "the apply link lands in a real requisition queue" undermines the thesis.
//
//	seed              register boards and demo users
//	seed -ingest      also fetch everything now, synchronously
//	seed -reset       wipe postings first
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jobtrack/jobtrack/internal/app"
	"github.com/jobtrack/jobtrack/internal/auth"
	"github.com/jobtrack/jobtrack/internal/jobs"
	"github.com/jobtrack/jobtrack/internal/normalise"
	"github.com/jobtrack/jobtrack/internal/seed"
	"github.com/jobtrack/jobtrack/internal/store"
)

func main() {
	reset := flag.Bool("reset", false, "delete existing postings before seeding")
	ingest := flag.Bool("ingest", false, "fetch every registered board immediately")
	users := flag.Bool("users", true, "create demo users")
	flag.Parse()

	ctx := context.Background()
	a, err := app.New(ctx, app.Options{Service: "seed", NeedsDB: true})
	if err != nil {
		app.Fatal(err)
	}
	defer a.Close(ctx)

	if a.Cfg.Env == "prod" {
		app.Fatal(fmt.Errorf("refusing to seed a production database"))
	}

	if err := run(ctx, a, *reset, *ingest, *users); err != nil {
		a.Log.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, a *app.App, reset, ingest, withUsers bool) error {
	vocab := normalise.DefaultVocabulary()

	if err := store.EnsureSkills(ctx, a.Pool, seed.SkillCategories()); err != nil {
		return err
	}

	if reset {
		// Children before parents, or the foreign keys reject it. Companies and
		// sources are kept: re-registering them is idempotent, and dropping
		// them would discard the polling state that makes conditional requests
		// work.
		for _, table := range []string{"user_job_scores", "posting_skills", "posting_observations", "job_postings"} {
			if _, err := a.Pool.Exec(ctx, "DELETE FROM "+table); err != nil {
				return fmt.Errorf("reset %s: %w", table, err)
			}
		}
		// Clear the validators too, or every board returns 304 and the reset
		// leaves an empty database.
		if _, err := a.Pool.Exec(ctx,
			`UPDATE sources SET etag = NULL, last_modified = NULL, content_hash = NULL,
			                    next_poll_at = now(), consecutive_errors = 0, disabled_until = NULL`); err != nil {
			return err
		}
		a.Log.Info("existing postings removed")
	}

	registered, err := registerBoards(ctx, a)
	if err != nil {
		return err
	}
	pruned, err := pruneBoards(ctx, a)
	if err != nil {
		return err
	}
	a.Log.Info("boards registered", "count", registered, "pruned", pruned)

	if withUsers {
		if err := seedUsers(ctx, a); err != nil {
			return err
		}
	}

	if !ingest {
		a.Log.Info("seed complete — start the ingestor, or re-run with -ingest to fetch now")
		return nil
	}
	return fetchAll(ctx, a, vocab)
}

func registerBoards(ctx context.Context, a *app.App) (int, error) {
	var n int
	for _, b := range seed.Boards() {
		var companyID int64
		err := a.Pool.QueryRow(ctx, `
			INSERT INTO companies (slug, name, website, primary_domain, hq_country)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name, updated_at = now()
			RETURNING id`,
			b.Slug, b.Name, "https://"+b.Domain, b.Domain, b.Country).Scan(&companyID)
		if err != nil {
			return n, fmt.Errorf("upsert company %s: %w", b.Slug, err)
		}

		// Tier A so a fresh install fetches everything promptly. The retier job
		// moves quiet boards down within the hour.
		if _, err := a.Pool.Exec(ctx, `
			INSERT INTO sources (company_id, vendor, board_token, tier, next_poll_at)
			VALUES ($1, $2::source_vendor, $3, 'a', now())
			ON CONFLICT (vendor, board_token) DO UPDATE
			   SET company_id = EXCLUDED.company_id, updated_at = now()`,
			companyID, string(b.Vendor), b.Token); err != nil {
			return n, fmt.Errorf("upsert source %s/%s: %w", b.Vendor, b.Token, err)
		}
		n++
	}
	return n, nil
}

// pruneBoards removes sources that are no longer in the curated list.
//
// This exists because of a real bug: an earlier revision registered board
// tokens guessed from company names, most of which did not exist. Those rows
// stayed behind after the list was corrected, and a source row that resolves to
// nothing is not harmless — the fetcher keeps polling it, the circuit breaker
// keeps tripping, and the error rate looks like a vendor outage rather than
// stale configuration.
//
// Postings are removed with them: a posting whose board no longer exists cannot
// be applied to, and showing it is worse than showing nothing.
func pruneBoards(ctx context.Context, a *app.App) (int64, error) {
	tokens := make([]string, 0, len(seed.Boards()))
	for _, b := range seed.Boards() {
		tokens = append(tokens, string(b.Vendor)+":"+b.Token)
	}

	tag, err := a.Pool.Exec(ctx, `
		WITH doomed AS (
			SELECT id FROM sources WHERE vendor::text || ':' || board_token <> ALL($1)
		), _p AS (
			DELETE FROM job_postings WHERE source_id IN (SELECT id FROM doomed)
		)
		DELETE FROM sources WHERE id IN (SELECT id FROM doomed)`, tokens)
	if err != nil {
		return 0, fmt.Errorf("prune sources: %w", err)
	}

	// Companies left with no sources are dead weight too.
	if _, err := a.Pool.Exec(ctx, `
		DELETE FROM companies c
		 WHERE NOT EXISTS (SELECT 1 FROM sources s WHERE s.company_id = c.id)`); err != nil {
		return 0, fmt.Errorf("prune companies: %w", err)
	}
	return tag.RowsAffected(), nil
}

// fetchAll runs ingestion synchronously.
//
// Uses the same worker code the ingestor runs, rather than a parallel
// implementation — a seed path that fetched differently from production would
// eventually diverge and hide bugs in whichever one is used less.
func fetchAll(ctx context.Context, a *app.App, vocab *normalise.Vocabulary) error {
	deps := &jobs.Deps{Pool: a.Pool, Log: a.Log, Cfg: a.Cfg, Vocab: vocab}

	client, err := jobs.New(ctx, deps, jobs.RoleIngestor)
	if err != nil {
		return err
	}
	if err := jobs.Migrate(ctx, deps); err != nil {
		return err
	}

	rows, err := a.Pool.Query(ctx, `SELECT id FROM sources WHERE tier <> 'paused' ORDER BY id`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()

	a.Log.Info("fetching boards", "count", len(ids))

	if err := client.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = client.Stop(context.WithoutCancel(ctx)) }()

	for _, id := range ids {
		if _, err := client.Insert(ctx, jobs.FetchSourceArgs{SourceID: id}, nil); err != nil {
			return fmt.Errorf("enqueue fetch for source %d: %w", id, err)
		}
	}

	// Wait for the queue to drain rather than exiting immediately, so `-ingest`
	// means "the data is there when this returns".
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		var pending int
		if err := a.Pool.QueryRow(ctx, `
			SELECT count(*) FROM river_job
			 WHERE kind IN ('fetch_source','dedupe_company')
			   AND state IN ('available','running','retryable','scheduled')`).Scan(&pending); err != nil {
			return err
		}
		if pending == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	var postings, companies int
	_ = a.Pool.QueryRow(ctx, `SELECT count(*) FROM job_postings WHERE status = 'live'`).Scan(&postings)
	_ = a.Pool.QueryRow(ctx, `SELECT count(DISTINCT company_id) FROM job_postings WHERE status = 'live'`).Scan(&companies)

	a.Log.Info("ingestion complete", "live_postings", postings, "companies", companies)
	return nil
}

// seedUsers creates the demo accounts, already onboarded so the dashboard has
// something to show on first login.
func seedUsers(ctx context.Context, a *app.App) error {
	hash, err := auth.HashPassword("dev-password-please")
	if err != nil {
		return err
	}

	type demo struct {
		Email, First, Last, Title, Target string
		YoE                               float64
		Countries, Modes                  []string
		CompMin                           float64
		Currency                          string
		Skills                            []string
		Prefs                             string
	}

	demos := []demo{
		{
			Email: "dev@jobtrack.local", First: "Arjun", Last: "Mehta",
			Title: "Software Engineer", Target: "Senior Backend Engineer",
			YoE: 2, Countries: []string{"IN"}, Modes: []string{"remote", "hybrid", "onsite"},
			CompMin: 2_500_000, Currency: "INR",
			Skills: []string{"python", "postgresql", "docker", "aws", "django", "linux", "git"},
			Prefs:  `{"theme":"system","density":"compact"}`,
		},
		{
			Email: "senior@jobtrack.local", First: "Priya", Last: "Sharma",
			Title: "Senior Backend Engineer", Target: "Staff Engineer",
			YoE: 7, Countries: []string{"IN", "GB"}, Modes: []string{"remote"},
			CompMin: 6_000_000, Currency: "INR",
			Skills: []string{"go", "kubernetes", "kafka", "postgresql", "terraform", "observability", "aws"},
			Prefs:  `{"theme":"dark","density":"compact"}`,
		},
		{
			Email: "grad@jobtrack.local", First: "Sam", Last: "Okafor",
			Title: "", Target: "Software Engineer",
			YoE: 0, Countries: []string{"US", "GB"}, Modes: []string{"remote", "hybrid"},
			CompMin: 90_000, Currency: "USD",
			Skills: []string{"javascript", "typescript", "react", "git", "sql"},
			Prefs:  `{"theme":"light","density":"comfortable"}`,
		},
	}

	for _, d := range demos {
		var userID int64
		err := a.Pool.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, email_verified_at, preferences,
			                   first_name, last_name, current_title, target_title,
			                   total_yoe, pref_countries, pref_modes, pref_comp_min,
			                   pref_currency, onboarded_at, last_active_at)
			VALUES ($1,$2,now(),$3::jsonb,$4,$5,NULLIF($6,''),$7,$8,$9,$10::work_mode[],$11,$12,now(),now())
			ON CONFLICT (email) DO UPDATE
			   SET password_hash = EXCLUDED.password_hash,
			       preferences = EXCLUDED.preferences,
			       first_name = EXCLUDED.first_name,
			       last_name = EXCLUDED.last_name,
			       current_title = EXCLUDED.current_title,
			       target_title = EXCLUDED.target_title,
			       total_yoe = EXCLUDED.total_yoe,
			       pref_countries = EXCLUDED.pref_countries,
			       pref_modes = EXCLUDED.pref_modes,
			       pref_comp_min = EXCLUDED.pref_comp_min,
			       pref_currency = EXCLUDED.pref_currency,
			       onboarded_at = COALESCE(users.onboarded_at, now()),
			       last_active_at = now(),
			       updated_at = now()
			RETURNING id`,
			d.Email, hash, d.Prefs, d.First, d.Last, d.Title, d.Target,
			d.YoE, d.Countries, d.Modes, d.CompMin, d.Currency).Scan(&userID)
		if err != nil {
			return fmt.Errorf("seed user %s: %w", d.Email, err)
		}

		if _, err := a.Pool.Exec(ctx, `
			INSERT INTO user_skills (user_id, skill_id, origin)
			SELECT $1, s.id, 'user' FROM skills s WHERE s.canonical = ANY($2)
			ON CONFLICT (user_id, skill_id) DO NOTHING`, userID, d.Skills); err != nil {
			return fmt.Errorf("seed skills for %s: %w", d.Email, err)
		}
	}

	a.Log.Info("demo users ready", "count", len(demos), "password", "dev-password-please")
	return nil
}
