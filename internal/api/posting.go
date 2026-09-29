package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/httpx"
	"github.com/ergodicregulus/jobtrack/internal/matching"
	"github.com/ergodicregulus/jobtrack/internal/store"
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

	row, err := store.LoadPostingDetail(ctx, a.pool, a.scorer, id, viewer)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return httpx.ErrNotFound()
	case err != nil:
		return httpx.ErrInternal(err)
	}

	p := Posting{
		ID: row.ID, Title: row.Title, CompanyName: row.CompanyName,
		CompanySlug: row.CompanySlug, LocationRaw: row.LocationRaw, Mode: row.Mode,
		ApplyURL: row.ApplyURL, Vendor: row.Vendor, DescriptionHTML: row.DescriptionHTML,
		CompMin: row.CompMin, CompMax: row.CompMax, CompCurrency: row.CompCurrency,
		CompPeriod: row.CompPeriod, CompSource: row.CompSource,
		YoEMin: row.YoEMin, YoEMax: row.YoEMax, YoEConfidence: row.YoEConfidence,
		PostedAt: row.PostedAt, PostedAtIsEstimate: row.PostedAtIsEstimate,
		FirstSeenAt:          row.FirstSeenAt,
		AIScreeningDisclosed: row.AIScreeningDisclosed, AIDisclaimer: row.AIDisclaimer,
		AIOptOutURL: row.AIOptOutURL, ParseConfidence: row.ParseConfidence,
		MustHaveSkills: row.MustHaveSkills, NiceToHaveSkills: row.NiceToHaveSkills,
		Saved: row.Saved,
	}
	if row.Match != nil {
		p.Match = wireMatch(*row.Match)
	}

	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, p)
	return nil
}

// wireMatch shapes a scoring result for the API.
//
// The components are the ones this request computed, so what a reader sees is
// necessarily what produced the number beside it. When they were stored, a
// scorer change and a stale row could disagree — which is the failure ADR-0011
// documents and the reason profile_version existed.
func wireMatch(r matching.Result) *PostingMatch {
	m := &PostingMatch{
		Score:      r.Score,
		Band:       string(r.Band),
		Confidence: float64(r.Confidence),
		Missing:    r.MissingSkills(),
		ComputedAt: time.Now(),
		Components: make([]MatchComponent, 0, len(r.Components)),
	}
	for _, c := range r.Components {
		m.Components = append(m.Components, MatchComponent{
			Name:    c.Name,
			Score:   c.Score,
			Max:     c.Max,
			Detail:  c.Detail,
			Neutral: c.Max > 0 && c.Score == 0 && c.Evidence == 0,
			Matched: c.Matched,
			Missing: c.Missing,
		})
	}
	return m
}
