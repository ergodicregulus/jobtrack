package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/jobtrack/jobtrack/internal/matching"
)

// ScorePostingWorker scores one posting for the users it could plausibly suit.
//
// The fan-out is bounded here, and that boundary is the single most important
// capacity decision in the system. Scoring every posting against every user is
// O(users × postings) and would dominate everything else, so a posting is only
// scored for users whose coarse preferences make it relevant AND who have been
// active recently — nobody is waiting on a score they will never look at.
type ScorePostingWorker struct {
	river.WorkerDefaults[ScorePostingArgs]
	Deps   *Deps
	Scorer *matching.Scorer
}

// activeWindow bounds fan-out to users who might actually see the result.
// Dormant users are scored lazily on their next session instead.
const activeWindow = 30 * 24 * time.Hour

func (w *ScorePostingWorker) Work(ctx context.Context, job *river.Job[ScorePostingArgs]) error {
	posting, err := loadPosting(ctx, w.Deps, job.Args.PostingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // closed or deleted between enqueue and run
	}
	if err != nil {
		return err
	}

	profiles, err := w.candidateProfiles(ctx, posting)
	if err != nil {
		return err
	}

	if err := upsertScores(ctx, w.Deps, posting, profiles, w.Scorer); err != nil {
		return err
	}

	// Drop score rows this run did not write.
	//
	// Candidate selection narrows by geography and recent activity, so a user
	// who changes their country preference — or simply stops logging in — leaves
	// behind a row that will never be selected again. Without this, that row is
	// immortal: it keeps its old score forever, and the stale-version sweep
	// re-enqueues this posting every five minutes trying to fix a row it can
	// never reach. The sweep would never converge.
	//
	// Everyone still eligible was just written with the current version, so
	// anything left on an older version is by definition no longer applicable.
	// Deleting it is also the correct answer on its own terms: a score the
	// current rules would not produce should not be shown.
	if _, err := w.Deps.Pool.Exec(ctx,
		`DELETE FROM user_job_scores WHERE posting_id = $1 AND profile_version <> $2`,
		posting.ID, w.Scorer.Version()); err != nil {
		return fmt.Errorf("prune superseded scores for posting %d: %w", posting.ID, err)
	}

	if len(profiles) == 0 {
		return nil
	}

	// The fan-out ratio is a DERIVED estimate, not a measurement — and it was
	// wrong by 4x once already. Recording it per job is how it stops being a
	// guess the capacity model silently depends on.
	w.Deps.Log.DebugContext(ctx, "posting scored",
		"posting_id", posting.ID, "users", len(profiles))
	return nil
}

// candidateProfiles selects users this posting could plausibly suit.
func (w *ScorePostingWorker) candidateProfiles(ctx context.Context, j matching.Posting) ([]matching.Profile, error) {
	const q = `
SELECT u.id, COALESCE(u.total_yoe, 0), u.pref_countries, u.pref_modes,
       COALESCE(u.pref_comp_min, 0), COALESCE(u.pref_currency, ''),
       COALESCE(
         (SELECT jsonb_object_agg(s.canonical, COALESCE(us.years, 0))
            FROM user_skills us JOIN skills s ON s.id = us.skill_id
           WHERE us.user_id = u.id),
         '{}'::jsonb)
  FROM users u
 WHERE u.deleted_at IS NULL
   AND u.onboarded_at IS NOT NULL
   -- Active users only. This is the difference between ~4 and ~12 sustained
   -- cores at year-2 volume, and it costs nothing in user-visible quality.
   AND u.last_active_at > now() - $1::interval
   -- Coarse geography match. A remote posting suits everyone, and a user who
   -- stated no preference has not ruled anything out.
   AND (
        $2 = 'remote'
     OR cardinality(u.pref_countries) = 0
     OR $3 = ''
     OR $3 = ANY(u.pref_countries)
   )`

	rows, err := w.Deps.Pool.Query(ctx, q, activeWindow.String(), j.Mode, j.Country)
	if err != nil {
		return nil, fmt.Errorf("select candidate profiles: %w", err)
	}
	defer rows.Close()

	var out []matching.Profile
	for rows.Next() {
		var (
			p         matching.Profile
			countries []string
			modes     []string
			skills    []byte
		)
		if err := rows.Scan(&p.UserID, &p.TotalYoE, &countries, &modes,
			&p.CompMin, &p.Currency, &skills); err != nil {
			return nil, err
		}
		p.Countries, p.Modes = countries, modes
		p.ParseConfidence = 1.0 // self-declared profile; a resume would lower this
		p.Skills = map[string]float64{}
		if len(skills) > 0 {
			_ = json.Unmarshal(skills, &p.Skills)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ScoreUserWorker rescores every live posting for one user.
//
// Runs on the low-priority queue: a full rescore after onboarding must never
// starve live scoring of newly-ingested postings.
type ScoreUserWorker struct {
	river.WorkerDefaults[ScoreUserArgs]
	Deps   *Deps
	Scorer *matching.Scorer
}

func (w *ScoreUserWorker) Work(ctx context.Context, job *river.Job[ScoreUserArgs]) error {
	profile, err := loadProfile(ctx, w.Deps, job.Args.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	// Cap the work. Storing every score for every user is what makes
	// user_job_scores the fastest-growing table; the top N by recency is what
	// the user will actually page through.
	const cap = 2000

	rows, err := w.Deps.Pool.Query(ctx, `
		SELECT p.id, p.yoe_min, p.yoe_max, p.yoe_confidence,
		       COALESCE(p.country,''), p.mode::text,
		       p.comp_min, p.comp_max, COALESCE(p.comp_currency,''),
		       COALESCE(p.posted_at, p.first_seen_at), p.parse_confidence,
		       COALESCE((SELECT array_agg(s.canonical)
		                   FROM posting_skills ps JOIN skills s ON s.id = ps.skill_id
		                  WHERE ps.posting_id = p.id AND ps.requirement = 'must_have'), '{}'),
		       -- Nice-to-haves, falling back to merely-mentioned skills when the
		       -- posting has no requirements section we could parse.
		       --
		       -- 73.7% of everything the extractor finds lands as 'mentioned',
		       -- because the must/nice split needs an explicit "Requirements"
		       -- heading and most postings do not have one. Discarding all of it
		       -- meant 28,056 successfully-extracted skills were thrown away and
		       -- the skills component abstained on three postings in four.
		       --
		       -- "The posting talks about Kafka" is weaker evidence than "the
		       -- posting requires Kafka", and it is not nothing. Treating it as a
		       -- nice-to-have weights it lower, and the evidence-weighting in the
		       -- scorer damps it further because such a posting states little.
		       -- Where a real requirements section exists this changes nothing.
		       COALESCE((
		         SELECT array_agg(s.canonical)
		           FROM posting_skills ps JOIN skills s ON s.id = ps.skill_id
		          WHERE ps.posting_id = p.id
		            AND ps.requirement = CASE
		                  WHEN EXISTS (
		                    SELECT 1 FROM posting_skills q
		                     WHERE q.posting_id = p.id
		                       AND q.requirement IN ('must_have','nice_to_have')
		                  ) THEN 'nice_to_have'::skill_requirement
		                  ELSE 'mentioned'::skill_requirement
		                END
		       ), '{}')
		  FROM job_postings p
		 WHERE p.status = 'live'
		 ORDER BY COALESCE(p.posted_at, p.first_seen_at) DESC
		 LIMIT $1`, cap)
	if err != nil {
		return fmt.Errorf("select postings to score: %w", err)
	}
	defer rows.Close()

	var (
		scored int
		batch  = make([]scoreRow, 0, scoreFlushSize)
	)
	for rows.Next() {
		j, err := scanPosting(rows)
		if err != nil {
			return err
		}
		row, err := newScoreRow(profile.UserID, j, w.Scorer.Score(profile, j))
		if err != nil {
			return err
		}
		batch = append(batch, row)
		if len(batch) == scoreFlushSize {
			if err := flushScores(ctx, w.Deps, batch, w.Scorer.Version()); err != nil {
				return err
			}
			batch = batch[:0]
		}
		scored++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := flushScores(ctx, w.Deps, batch, w.Scorer.Version()); err != nil {
		return err
	}

	w.Deps.Log.InfoContext(ctx, "user rescored", "user_id", profile.UserID, "postings", scored)
	return nil
}

// --- shared helpers ---------------------------------------------------------

func loadPosting(ctx context.Context, d *Deps, id int64) (matching.Posting, error) {
	row := d.Pool.QueryRow(ctx, `
		SELECT p.id, p.yoe_min, p.yoe_max, p.yoe_confidence,
		       COALESCE(p.country,''), p.mode::text,
		       p.comp_min, p.comp_max, COALESCE(p.comp_currency,''),
		       COALESCE(p.posted_at, p.first_seen_at), p.parse_confidence,
		       COALESCE((SELECT array_agg(s.canonical)
		                   FROM posting_skills ps JOIN skills s ON s.id = ps.skill_id
		                  WHERE ps.posting_id = p.id AND ps.requirement = 'must_have'), '{}'),
		       -- Nice-to-haves, falling back to merely-mentioned skills when the
		       -- posting has no requirements section we could parse.
		       --
		       -- 73.7% of everything the extractor finds lands as 'mentioned',
		       -- because the must/nice split needs an explicit "Requirements"
		       -- heading and most postings do not have one. Discarding all of it
		       -- meant 28,056 successfully-extracted skills were thrown away and
		       -- the skills component abstained on three postings in four.
		       --
		       -- "The posting talks about Kafka" is weaker evidence than "the
		       -- posting requires Kafka", and it is not nothing. Treating it as a
		       -- nice-to-have weights it lower, and the evidence-weighting in the
		       -- scorer damps it further because such a posting states little.
		       -- Where a real requirements section exists this changes nothing.
		       COALESCE((
		         SELECT array_agg(s.canonical)
		           FROM posting_skills ps JOIN skills s ON s.id = ps.skill_id
		          WHERE ps.posting_id = p.id
		            AND ps.requirement = CASE
		                  WHEN EXISTS (
		                    SELECT 1 FROM posting_skills q
		                     WHERE q.posting_id = p.id
		                       AND q.requirement IN ('must_have','nice_to_have')
		                  ) THEN 'nice_to_have'::skill_requirement
		                  ELSE 'mentioned'::skill_requirement
		                END
		       ), '{}')
		  FROM job_postings p
		 WHERE p.id = $1 AND p.status = 'live'`, id)
	return scanPosting(row)
}

type scannable interface{ Scan(...any) error }

func scanPosting(row scannable) (matching.Posting, error) {
	var j matching.Posting
	err := row.Scan(&j.ID, &j.YoEMin, &j.YoEMax, &j.YoEConfidence,
		&j.Country, &j.Mode, &j.CompMin, &j.CompMax, &j.CompCurrency,
		&j.PostedAt, &j.ParseConfidence, &j.MustHaveSkills, &j.NiceToHaveSkills)
	return j, err
}

func loadProfile(ctx context.Context, d *Deps, userID int64) (matching.Profile, error) {
	var (
		p         matching.Profile
		countries []string
		modes     []string
		skills    []byte
	)
	err := d.Pool.QueryRow(ctx, `
		SELECT u.id, COALESCE(u.total_yoe,0), u.pref_countries, u.pref_modes,
		       COALESCE(u.pref_comp_min,0), COALESCE(u.pref_currency,''),
		       COALESCE(
		         (SELECT jsonb_object_agg(s.canonical, COALESCE(us.years,0))
		            FROM user_skills us JOIN skills s ON s.id = us.skill_id
		           WHERE us.user_id = u.id),
		         '{}'::jsonb)
		  FROM users u
		 WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
		Scan(&p.UserID, &p.TotalYoE, &countries, &modes, &p.CompMin, &p.Currency, &skills)
	if err != nil {
		return p, err
	}

	p.Countries, p.Modes = countries, modes
	p.ParseConfidence = 1.0
	p.Skills = map[string]float64{}
	if len(skills) > 0 {
		_ = json.Unmarshal(skills, &p.Skills)
	}
	return p, nil
}

// upsertScore writes one score.
//
// profile_version is stamped on every row so we can always answer "which model
// produced this?" — without it, a weight change silently produces a feed mixing
// old and new scores with no way to tell them apart.
// scoreRow is one user's score for one posting, shaped for the bulk insert.
// The JSON tags are the column names jsonb_to_recordset destructures below.
type scoreRow struct {
	UserID        int64           `json:"user_id"`
	PostingID     int64           `json:"posting_id"`
	Score         float64         `json:"score"`
	Band          string          `json:"band"`
	Components    json.RawMessage `json:"components"`
	Confidence    float64         `json:"confidence"`
	MissingSkills []string        `json:"missing_skills"`
}

// scoreFlushSize bounds one statement's payload.
//
// The saving is round trips, and collapsing 500 into 1 already takes that cost
// to nothing; a larger batch would only grow the JSON document, which is held
// in memory twice — once in Go, once in the server's parse. A user rescore
// sweeps the whole live corpus, so this must be bounded by something.
const scoreFlushSize = 500

func newScoreRow(userID int64, posting matching.Posting, r matching.Result) (scoreRow, error) {
	components, err := json.Marshal(r.Components)
	if err != nil {
		return scoreRow{}, fmt.Errorf("marshal components user=%d posting=%d: %w",
			userID, posting.ID, err)
	}
	return scoreRow{
		UserID:        userID,
		PostingID:     posting.ID,
		Score:         float64(r.Score),
		Band:          string(r.Band),
		Components:    components,
		Confidence:    float64(r.Confidence),
		MissingSkills: missingSkills(r),
	}, nil
}

// flushScores writes a batch of scores in a single statement.
//
// Every caller here fans out — one posting across every active user, or one
// user across the whole live corpus — so a per-row round trip makes the cost of
// scoring linear in network latency rather than in computation. It was exactly
// that: 32 seconds to score one posting for 62 users, ten workers, 14,700 jobs
// queued on the interactive queue, and GET /v1/me/dashboard timing out at ten
// seconds behind them.
//
// jsonb_to_recordset rather than unnest() because missing_skills is itself an
// array per row, and Postgres arrays are rectangular: a ragged array of arrays
// has no unnest form, while JSON carries it exactly.
func flushScores(ctx context.Context, d *Deps, rows []scoreRow, version string) error {
	if len(rows) == 0 {
		return nil
	}

	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("marshal %d scores: %w", len(rows), err)
	}

	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO user_job_scores
		    (user_id, posting_id, resume_id, score, band, components, confidence,
		     profile_version, missing_skills)
		SELECT x.user_id, x.posting_id, NULL, x.score, x.band, x.components,
		       x.confidence, $2, x.missing_skills
		  FROM jsonb_to_recordset($1::jsonb) AS x(
		       user_id bigint, posting_id bigint, score real, band text,
		       components jsonb, confidence real, missing_skills text[])
		ON CONFLICT (user_id, posting_id) DO UPDATE
		   SET score = EXCLUDED.score,
		       band = EXCLUDED.band,
		       components = EXCLUDED.components,
		       confidence = EXCLUDED.confidence,
		       profile_version = EXCLUDED.profile_version,
		       missing_skills = EXCLUDED.missing_skills,
		       computed_at = now()`,
		payload, version); err != nil {
		return fmt.Errorf("upsert %d scores: %w", len(rows), err)
	}
	return nil
}

// upsertScores writes every score for one posting in a single statement.
//
// This was one Exec per user inside a loop. Each is a round trip, so the cost
// of scoring a posting was linear in the number of candidates *in network
// latency* rather than in computation: measured at 32s per posting for 62
// users, with ten workers, which put 14,700 jobs on the live queue and left
// GET /v1/me/dashboard timing out at ten seconds. Fan-out is the whole shape of
// this workload — one posting is scored against every active user — so it is
// the one place a per-row round trip must not appear.
//
// jsonb_to_recordset rather than unnest() because missing_skills is itself an
// array per row, and Postgres arrays are rectangular: a ragged array of arrays
// has no unnest form, while JSON carries it exactly.
func upsertScores(ctx context.Context, d *Deps, posting matching.Posting,
	profiles []matching.Profile, scorer *matching.Scorer) error {
	rows := make([]scoreRow, 0, len(profiles))
	for _, p := range profiles {
		row, err := newScoreRow(p.UserID, posting, scorer.Score(p, posting))
		if err != nil {
			return err
		}
		rows = append(rows, row)
		if len(rows) == scoreFlushSize {
			if err := flushScores(ctx, d, rows, scorer.Version()); err != nil {
				return err
			}
			rows = rows[:0]
		}
	}
	return flushScores(ctx, d, rows, scorer.Version())
}

// missingSkills lifts the skills gap out of the component breakdown so the feed
// can render it without unnesting JSON per row.
//
// Always returns a non-nil slice: a NULL column means "not scored since this
// column existed", and a zero-length array means "nothing missing". Collapsing
// those two would make a perfect match indistinguishable from a stale row.
func missingSkills(r matching.Result) []string {
	for _, c := range r.Components {
		if c.Name == "skills" {
			if c.Missing == nil {
				return []string{}
			}
			return c.Missing
		}
	}
	return []string{}
}
