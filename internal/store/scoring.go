package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/matching"
)

// The scoring queries live here rather than in internal/jobs for the reason
// ADR-0015 gives, and one specific to this pair: the posting projection below
// was written out twice, once for the bulk rescore and once for the single
// lookup. Two copies of a thirty-line SELECT drift, and the nice-to-have
// fallback in the middle of it is the most consequential piece of SQL in the
// product. It is now written once.

// postingScoringColumns is what the scorer needs to judge a posting.
const postingScoringColumns = `p.id, p.yoe_min, p.yoe_max,
       -- COALESCE for the same reason as the feed and the detail query:
       -- yoe_confidence is nullable and the scorer's field is a plain float64.
       -- Found by TestReadsSurviveAllNullableColumns after the identical bug
       -- had already been fixed twice elsewhere, which is the argument for
       -- having a test for the class rather than fixing instances.
       COALESCE(p.yoe_confidence, 0),
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
		       ), '{}')`

const (
	postingsToScoreSQL = `SELECT ` + postingScoringColumns + `
		  FROM job_postings p
		 WHERE p.status = 'live'
		 ORDER BY COALESCE(p.posted_at, p.first_seen_at) DESC
		 LIMIT $1`

	postingToScoreSQL = `SELECT ` + postingScoringColumns + `
		  FROM job_postings p
		 WHERE p.id = $1 AND p.status = 'live'`

	candidateProfilesSQL = `
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

	profileForScoringSQL = `
		SELECT u.id, COALESCE(u.total_yoe,0), u.pref_countries, u.pref_modes,
		       COALESCE(u.pref_comp_min,0), COALESCE(u.pref_currency,''),
		       COALESCE(
		         (SELECT jsonb_object_agg(s.canonical, COALESCE(us.years,0))
		            FROM user_skills us JOIN skills s ON s.id = us.skill_id
		           WHERE us.user_id = u.id),
		         '{}'::jsonb)
		  FROM users u
		 WHERE u.id = $1 AND u.deleted_at IS NULL`

	upsertScoresSQL = `
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
		       computed_at = now()`

	pruneSupersededSQL = `DELETE FROM user_job_scores WHERE posting_id = $1 AND profile_version <> $2`
)

type scannable interface{ Scan(...any) error }

func scanPosting(row scannable) (matching.Posting, error) {
	var j matching.Posting
	err := row.Scan(&j.ID, &j.YoEMin, &j.YoEMax, &j.YoEConfidence,
		&j.Country, &j.Mode, &j.CompMin, &j.CompMax, &j.CompCurrency,
		&j.PostedAt, &j.ParseConfidence, &j.MustHaveSkills, &j.NiceToHaveSkills)
	return j, err
}

// PostingForScoring reads one live posting in the shape the scorer wants.
func PostingForScoring(ctx context.Context, pool *pgxpool.Pool, id int64) (matching.Posting, error) {
	return scanPosting(pool.QueryRow(ctx, postingToScoreSQL, id))
}

// PostingsForScoring reads the most recently posted live roles, newest first.
//
// Capped by the caller: storing every score for every user is what makes
// user_job_scores the fastest-growing table, and the top N by recency is what a
// user will actually page through.
func PostingsForScoring(ctx context.Context, pool *pgxpool.Pool, limit int) ([]matching.Posting, error) {
	rows, err := pool.Query(ctx, postingsToScoreSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("select postings to score: %w", err)
	}
	defer rows.Close()

	var out []matching.Posting
	for rows.Next() {
		p, err := scanPosting(rows)
		if err != nil {
			return nil, fmt.Errorf("scan posting: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProfileForScoring reads one user in the shape the scorer wants.
func ProfileForScoring(ctx context.Context, pool *pgxpool.Pool, userID int64) (matching.Profile, error) {
	return scanProfile(pool.QueryRow(ctx, profileForScoringSQL, userID))
}

// CandidateProfiles selects users a posting could plausibly suit.
//
// The active-user window is the difference between roughly 4 and roughly 12
// sustained cores at year-2 volume, and it costs nothing in user-visible
// quality: a dormant account's scores are recomputed the moment they return.
func CandidateProfiles(
	ctx context.Context,
	pool *pgxpool.Pool,
	activeWindow, mode, country string,
) ([]matching.Profile, error) {
	rows, err := pool.Query(ctx, candidateProfilesSQL, activeWindow, mode, country)
	if err != nil {
		return nil, fmt.Errorf("select candidate profiles: %w", err)
	}
	defer rows.Close()

	var out []matching.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan profile: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProfile(row scannable) (matching.Profile, error) {
	var (
		p         matching.Profile
		countries []string
		modes     []string
		skills    []byte
	)
	if err := row.Scan(&p.UserID, &p.TotalYoE, &countries, &modes,
		&p.CompMin, &p.Currency, &skills); err != nil {
		return p, err
	}

	p.Countries, p.Modes = countries, modes
	p.ParseConfidence = 1.0
	p.Skills = map[string]float64{}
	if len(skills) > 0 {
		// A malformed skills document must not fail a rescore: the profile is
		// still usable without it, and the scorer abstains on skills rather
		// than scoring them wrongly.
		_ = json.Unmarshal(skills, &p.Skills)
	}
	return p, nil
}

// ScoreRow is one computed score awaiting write.
type ScoreRow struct {
	UserID        int64           `json:"user_id"`
	PostingID     int64           `json:"posting_id"`
	Score         float64         `json:"score"`
	Band          string          `json:"band"`
	Components    json.RawMessage `json:"components"`
	Confidence    float64         `json:"confidence"`
	MissingSkills []string        `json:"missing_skills"`
}

// UpsertScores writes every score in one statement.
//
// This was one Exec per user inside a loop. Each is a round trip, so scoring a
// posting cost time linear in the number of candidates *in network latency*
// rather than in computation: 32s per posting for 62 users, which put 14,700
// jobs on the live queue and left GET /v1/me/dashboard timing out at ten
// seconds. Fan-out is the whole shape of this workload, so it is the one place
// a per-row round trip must not appear.
//
// jsonb_to_recordset rather than unnest() because missing_skills is itself an
// array per row, and Postgres arrays are rectangular: a ragged array of arrays
// has no unnest form, while JSON carries it exactly.
func UpsertScores(ctx context.Context, pool *pgxpool.Pool, rows []ScoreRow, version string) error {
	if len(rows) == 0 {
		return nil
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("marshal %d scores: %w", len(rows), err)
	}
	if _, err := pool.Exec(ctx, upsertScoresSQL, payload, version); err != nil {
		return fmt.Errorf("upsert %d scores: %w", len(rows), err)
	}
	return nil
}

// PruneSupersededScores deletes score rows left on an older profile version.
//
// Without this such a row is immortal: it keeps its old score forever, and the
// stale-version sweep re-enqueues the posting every five minutes trying to fix
// a row it can never reach, so the sweep never converges. Everyone still
// eligible was just written with the current version, so anything left behind
// is by definition no longer applicable — and a score the current rules would
// not produce should not be shown regardless.
func PruneSupersededScores(ctx context.Context, pool *pgxpool.Pool, postingID int64, version string) error {
	if _, err := pool.Exec(ctx, pruneSupersededSQL, postingID, version); err != nil {
		return fmt.Errorf("prune superseded scores for posting %d: %w", postingID, err)
	}
	return nil
}

const (
	staleScoresSQL = `
		SELECT DISTINCT s.posting_id
		  FROM user_job_scores s
		  JOIN job_postings p ON p.id = s.posting_id
		 WHERE s.profile_version <> $1
		   AND p.status = 'live'
		 LIMIT $2`

	sweepDeadScoresSQL = `
		DELETE FROM user_job_scores s
		 WHERE s.posting_id IN (
		   SELECT id FROM job_postings
		    WHERE status <> 'live'
		    LIMIT $1
		 )`
)

// StaleScorePostings finds postings carrying scores from an older model version.
//
// DISTINCT posting_id: one scoring job re-scores a posting for every user it
// suits, so enqueuing per (user, posting) row would multiply the work by the
// user count for no benefit.
func StaleScorePostings(ctx context.Context, pool *pgxpool.Pool, version string, limit int) ([]int64, error) {
	rows, err := pool.Query(ctx, staleScoresSQL, version, limit)
	if err != nil {
		return nil, fmt.Errorf("find stale scores: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("find stale scores: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SweepDeadScores deletes scores for postings that are no longer live.
//
// Bounded like everything else here: a delete taking a lock proportional to
// corpus size is an outage waiting for a big enough corpus.
func SweepDeadScores(ctx context.Context, pool *pgxpool.Pool, limit int) (int64, error) {
	tag, err := pool.Exec(ctx, sweepDeadScoresSQL, limit)
	if err != nil {
		return 0, fmt.Errorf("sweep dead scores: %w", err)
	}
	return tag.RowsAffected(), nil
}
