package store

import (
	"context"
	"encoding/json"

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

const profileForScoringSQL = `
	SELECT u.id, COALESCE(u.total_yoe,0), u.pref_countries, u.pref_modes,
	       COALESCE(u.pref_comp_min,0), COALESCE(u.pref_currency,''),
	       COALESCE(
	         (SELECT jsonb_object_agg(s.canonical, COALESCE(us.years,0))
	            FROM user_skills us JOIN skills s ON s.id = us.skill_id
	           WHERE us.user_id = u.id),
	         '{}'::jsonb)
	  FROM users u
	 WHERE u.id = $1 AND u.deleted_at IS NULL`

type scannable interface{ Scan(...any) error }

func scanPosting(row scannable) (matching.Posting, error) {
	var j matching.Posting
	err := row.Scan(&j.ID, &j.YoEMin, &j.YoEMax, &j.YoEConfidence,
		&j.Country, &j.Mode, &j.CompMin, &j.CompMax, &j.CompCurrency,
		&j.PostedAt, &j.ParseConfidence, &j.MustHaveSkills, &j.NiceToHaveSkills)
	return j, err
}

// ProfileForScoring reads one user in the shape the scorer wants.
func ProfileForScoring(ctx context.Context, pool *pgxpool.Pool, userID int64) (matching.Profile, error) {
	return scanProfile(pool.QueryRow(ctx, profileForScoringSQL, userID))
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

const ()
