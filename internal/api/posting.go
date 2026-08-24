package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jobtrack/jobtrack/internal/httpx"
)

// Posting is one job in full.
//
// The score breakdown is the reason this endpoint exists. The feed can show a
// number and a band; only here is there room for the five components and the
// one-line reason each of them earned, which is the product's whole argument
// that a match score should be interrogable rather than trusted.
type Posting struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	CompanyName string `json:"company_name"`
	CompanySlug string `json:"company_slug"`
	LocationRaw string `json:"location_raw"`
	Mode        string `json:"mode"`
	ApplyURL    string `json:"apply_url"`
	Vendor      string `json:"ats_vendor"`

	// DescriptionHTML is the employer's own markup, sanitised at ingest.
	DescriptionHTML string `json:"description_html"`

	CompMin      *float64 `json:"comp_min"`
	CompMax      *float64 `json:"comp_max"`
	CompCurrency *string  `json:"comp_currency"`
	CompPeriod   *string  `json:"comp_period"`
	// CompSource distinguishes a published salary field from one we read out of
	// the description. The second is a weaker claim and the UI says so.
	CompSource *string `json:"comp_source"`

	YoEMin        *int16  `json:"yoe_min"`
	YoEMax        *int16  `json:"yoe_max"`
	YoEConfidence float64 `json:"yoe_confidence"`

	PostedAt           *time.Time `json:"posted_at"`
	PostedAtIsEstimate bool       `json:"posted_at_is_estimate"`
	FirstSeenAt        time.Time  `json:"first_seen_at"`

	AIScreeningDisclosed *bool   `json:"ai_screening_disclosed"`
	AIDisclaimer         *string `json:"ai_disclaimer"`
	AIOptOutURL          *string `json:"ai_opt_out_url"`

	// ParseConfidence below 0.5 means we understood less than half the posting.
	// Shown rather than hidden: a reader deciding on incomplete information
	// should know the information is incomplete.
	ParseConfidence float64 `json:"parse_confidence"`

	MustHaveSkills   []string `json:"must_have_skills"`
	NiceToHaveSkills []string `json:"nice_to_have_skills"`

	// Match is absent for anonymous viewers and for postings not yet scored.
	// Those are different states and the UI distinguishes them.
	Match *PostingMatch `json:"match,omitempty"`
	Saved bool          `json:"saved"`
}

// PostingMatch carries the score AND its full reasoning.
type PostingMatch struct {
	Score      float64          `json:"score"`
	Band       string           `json:"band"`
	Confidence float64          `json:"confidence"`
	Missing    []string         `json:"missing_skills"`
	Components []MatchComponent `json:"components"`
	ComputedAt time.Time        `json:"computed_at"`
}

// MatchComponent is one weighted part of a score, with the reason it earned
// what it did.
type MatchComponent struct {
	Name    string   `json:"name"`
	Score   float64  `json:"score"`
	Max     float64  `json:"max"`
	Detail  string   `json:"detail"`
	Neutral bool     `json:"neutral"`
	Matched []string `json:"matched,omitempty"`
	Missing []string `json:"missing,omitempty"`
}

func (a *API) handlePosting(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	id, err := pathInt(r, "id")
	if err != nil {
		return err
	}

	// Viewer is optional: a posting is readable without an account, the score
	// is not. Binding NULL makes the joins miss, which is the correct anonymous
	// result and avoids a second query.
	var viewer any
	if uid := httpx.UserIDFromContext(ctx); uid != 0 {
		viewer = uid
	}

	var p Posting
	var componentsJSON []byte
	var score, confidence *float64
	var band *string
	var computedAt *time.Time
	var missing []string

	err = a.pool.QueryRow(ctx, `
		SELECT p.id, p.title, c.name, c.slug, COALESCE(p.location_raw, ''),
		       COALESCE(p.mode::text, ''), p.apply_url, s.vendor::text,
		       COALESCE(p.description_html, ''),
		       p.comp_min, p.comp_max, p.comp_currency, p.comp_period, p.comp_src::text,
		       p.yoe_min, p.yoe_max, p.yoe_confidence,
		       p.posted_at, p.posted_at_is_estimate, p.first_seen_at,
		       p.ai_screening_disclosed, p.ai_disclaimer, p.ai_opt_out_url,
		       p.parse_confidence,
		       COALESCE((SELECT array_agg(sk.display_name ORDER BY sk.display_name)
		                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
		                  WHERE ps.posting_id = p.id AND ps.requirement = 'must_have'), '{}'),
		       COALESCE((SELECT array_agg(sk.display_name ORDER BY sk.display_name)
		                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
		                  WHERE ps.posting_id = p.id AND ps.requirement = 'nice_to_have'), '{}'),
		       ujs.score, ujs.band, ujs.confidence, ujs.components, ujs.computed_at,
		       COALESCE(ujs.missing_skills, '{}'),
		       (app.user_id IS NOT NULL)
		  FROM job_postings p
		  JOIN companies c ON c.id = p.company_id
		  JOIN sources   s ON s.id = p.source_id
		  LEFT JOIN user_job_scores ujs
		         ON ujs.posting_id = p.id AND ujs.user_id = $2::bigint
		  LEFT JOIN applications app
		         ON app.posting_id = p.id AND app.user_id = $2::bigint
		 WHERE p.id = $1 AND p.status = 'live'`, id, viewer).
		Scan(&p.ID, &p.Title, &p.CompanyName, &p.CompanySlug, &p.LocationRaw,
			&p.Mode, &p.ApplyURL, &p.Vendor, &p.DescriptionHTML,
			&p.CompMin, &p.CompMax, &p.CompCurrency, &p.CompPeriod, &p.CompSource,
			&p.YoEMin, &p.YoEMax, &p.YoEConfidence,
			&p.PostedAt, &p.PostedAtIsEstimate, &p.FirstSeenAt,
			&p.AIScreeningDisclosed, &p.AIDisclaimer, &p.AIOptOutURL,
			&p.ParseConfidence, &p.MustHaveSkills, &p.NiceToHaveSkills,
			&score, &band, &confidence, &componentsJSON, &computedAt,
			&missing, &p.Saved)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Also the answer for a posting that closed: it is no longer live, and
		// saying "not found" is truer than showing a role nobody can apply to.
		return httpx.ErrNotFound()
	case err != nil:
		return httpx.ErrInternal(err)
	}

	if score != nil && band != nil {
		m := &PostingMatch{Score: *score, Band: *band, Missing: missing}
		if confidence != nil {
			m.Confidence = *confidence
		}
		if computedAt != nil {
			m.ComputedAt = *computedAt
		}
		// The breakdown is stored as the scorer wrote it. Decoding rather than
		// re-deriving means the reasons shown are the ones that actually
		// produced this score, not a recomputation that could disagree with it.
		if len(componentsJSON) > 0 {
			if err := json.Unmarshal(componentsJSON, &m.Components); err != nil {
				return httpx.ErrInternal(err)
			}
		}
		p.Match = m
	}
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, p)
	return nil
}
