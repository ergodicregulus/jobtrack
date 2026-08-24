package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostingDetail is one posting in full, with the viewer's score if there is one.
//
// The score fields are pointers because "not scored yet" and "scored zero" are
// different states and the interface distinguishes them. Collapsing them here
// would make that distinction impossible to recover further up.
type PostingDetail struct {
	ID          int64  `db:"id"`
	Title       string `db:"title"`
	CompanyName string `db:"company_name"`
	CompanySlug string `db:"company_slug"`
	LocationRaw string `db:"location_raw"`
	Mode        string `db:"mode"`
	ApplyURL    string `db:"apply_url"`
	Vendor      string `db:"ats_vendor"`

	// DescriptionHTML is the employer's own markup, sanitised at ingest.
	DescriptionHTML string `db:"description_html"`

	CompMin      *float64 `db:"comp_min"`
	CompMax      *float64 `db:"comp_max"`
	CompCurrency *string  `db:"comp_currency"`
	CompPeriod   *string  `db:"comp_period"`
	CompSource   *string  `db:"comp_source"`

	YoEMin        *int16  `db:"yoe_min"`
	YoEMax        *int16  `db:"yoe_max"`
	YoEConfidence float64 `db:"yoe_confidence"`

	PostedAt           *time.Time `db:"posted_at"`
	PostedAtIsEstimate bool       `db:"posted_at_is_estimate"`
	FirstSeenAt        time.Time  `db:"first_seen_at"`

	AIScreeningDisclosed *bool   `db:"ai_screening_disclosed"`
	AIDisclaimer         *string `db:"ai_disclaimer"`
	AIOptOutURL          *string `db:"ai_opt_out_url"`

	ParseConfidence float64 `db:"parse_confidence"`

	MustHaveSkills   []string `db:"must_have_skills"`
	NiceToHaveSkills []string `db:"nice_to_have_skills"`

	Score      *float64   `db:"score"`
	Band       *string    `db:"band"`
	Confidence *float64   `db:"confidence"`
	Components []byte     `db:"components"`
	ComputedAt *time.Time `db:"computed_at"`
	Missing    []string   `db:"missing_skills"`
	Saved      bool       `db:"saved"`
}

const postingDetailSQL = `
	SELECT p.id,
	       p.title,
	       c.name                        AS company_name,
	       c.slug                        AS company_slug,
	       COALESCE(p.location_raw, '')  AS location_raw,
	       COALESCE(p.mode::text, '')    AS mode,
	       p.apply_url,
	       s.vendor::text                AS ats_vendor,
	       COALESCE(p.description_html, '') AS description_html,
	       p.comp_min, p.comp_max, p.comp_currency, p.comp_period,
	       p.comp_src::text              AS comp_source,
	       p.yoe_min, p.yoe_max,
	       -- yoe_confidence is nullable with no default, and the wire type is a
	       -- plain float64, so a NULL could never have been represented anyway:
	       -- it would simply have failed the scan. No live row is NULL today
	       -- because UpsertPosting always sets it, but a data migration or a new
	       -- adapter inserting by another path would 500 this endpoint. Zero is
	       -- what NULL means here — we know nothing about the range.
	       COALESCE(p.yoe_confidence, 0) AS yoe_confidence,
	       p.posted_at, p.posted_at_is_estimate, p.first_seen_at,
	       p.ai_screening_disclosed, p.ai_disclaimer, p.ai_opt_out_url,
	       p.parse_confidence,
	       COALESCE((SELECT array_agg(sk.display_name ORDER BY sk.display_name)
	                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
	                  WHERE ps.posting_id = p.id AND ps.requirement = 'must_have'), '{}')
	                                     AS must_have_skills,
	       COALESCE((SELECT array_agg(sk.display_name ORDER BY sk.display_name)
	                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
	                  WHERE ps.posting_id = p.id AND ps.requirement = 'nice_to_have'), '{}')
	                                     AS nice_to_have_skills,
	       ujs.score, ujs.band, ujs.confidence, ujs.components, ujs.computed_at,
	       COALESCE(ujs.missing_skills, '{}') AS missing_skills,
	       (app.user_id IS NOT NULL)     AS saved
	  FROM job_postings p
	  JOIN companies c ON c.id = p.company_id
	  JOIN sources   s ON s.id = p.source_id
	  LEFT JOIN user_job_scores ujs
	         ON ujs.posting_id = p.id AND ujs.user_id = $2::bigint
	  LEFT JOIN applications app
	         ON app.posting_id = p.id AND app.user_id = $2::bigint
	 WHERE p.id = $1 AND p.status = 'live'`

// LoadPostingDetail reads one live posting for an optional viewer.
//
// viewer is nil for an anonymous reader, which makes both LEFT JOINs miss and
// leaves the score fields null — the same shape as a signed-in reader whose
// posting has not been scored yet.
//
// A posting that has closed returns ErrNotFound. That is truer than showing a
// role nobody can apply to.
func LoadPostingDetail(ctx context.Context, pool *pgxpool.Pool, id int64, viewer any) (PostingDetail, error) {
	rows, err := pool.Query(ctx, postingDetailSQL, id, viewer)
	if err != nil {
		return PostingDetail{}, fmt.Errorf("load posting: %w", err)
	}
	p, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[PostingDetail])
	if errors.Is(err, pgx.ErrNoRows) {
		return PostingDetail{}, ErrNotFound
	}
	if err != nil {
		return PostingDetail{}, fmt.Errorf("load posting: %w", err)
	}
	return p, nil
}

// PreviousVisit is the timestamp "new since you were last here" is measured from.
func PreviousVisit(ctx context.Context, pool *pgxpool.Pool, userID int64) (*time.Time, error) {
	var since *time.Time
	err := pool.QueryRow(ctx,
		`SELECT previous_visit_at FROM users WHERE id = $1`, userID).Scan(&since)
	if err != nil {
		return nil, fmt.Errorf("previous visit: %w", err)
	}
	return since, nil
}
