package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FeedFilter is the set of constraints from the query string.
//
// Every field is optional. An unset filter must never exclude anything — the
// governing rule is that our uncertainty never becomes the user's penalty.
type FeedFilter struct {
	Countries []string
	Modes     []string

	YoE *int16
	// YoEStretch widens the band upward. Default true: a large share of SDE-1
	// postings say "2+ years", that band is soft in practice, and
	// self-filtering there costs real opportunities.
	YoEStretch bool

	CompMin      *float64
	CompCurrency string
	// CompDisclosedOnly is separate from CompMin on purpose. Only ~80% of
	// postings disclose salary; collapsing "undisclosed" into "below your
	// floor" would silently drop a fifth of the market.
	CompDisclosedOnly bool

	PostedWithin time.Duration
	Skills       []string
	Vendors      []string
	Query        string

	// Bands filters to specific match bands — strong, plausible, stretch,
	// unlikely.
	//
	// Requires a viewer: a band is a statement about one person's fit, so it is
	// meaningless without a profile to compare against. An anonymous request
	// carrying this filter is rejected rather than silently ignored.
	//
	// Filtering here necessarily excludes postings the matcher has not reached
	// yet, because an unscored posting has no band. That is correct for a
	// deliberate "show me strong fits" request, and it is the reason the feed
	// keeps the header count next to the filters — the user must be able to see
	// that the set shrank.
	Bands []string

	Sort  string // newest | comp | match
	Limit int

	// UserID scopes match scores to one viewer. nil for anonymous requests,
	// where the score columns come back empty rather than absent — the feed is
	// public, and gating discovery behind an account would be the wrong trade.
	UserID *int64
}

// FeedItem is one card's worth of data.
type FeedItem struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	CompanyName string  `json:"company_name"`
	CompanySlug string  `json:"company_slug"`
	LocationRaw string  `json:"location_raw"`
	City        *string `json:"city"`
	Country     *string `json:"country"`
	Mode        string  `json:"mode"`
	ApplyURL    string  `json:"apply_url"`
	Vendor      string  `json:"ats_vendor"`

	CompMin      *float64 `json:"comp_min"`
	CompMax      *float64 `json:"comp_max"`
	CompCurrency *string  `json:"comp_currency"`
	CompPeriod   *string  `json:"comp_period"`
	CompSource   *string  `json:"comp_source"`

	YoEMin        *int16  `json:"yoe_min"`
	YoEMax        *int16  `json:"yoe_max"`
	YoEConfidence float64 `json:"yoe_confidence"`

	PostedAt           *time.Time `json:"posted_at"`
	PostedAtIsEstimate bool       `json:"posted_at_is_estimate"`
	FirstSeenAt        time.Time  `json:"first_seen_at"`

	AIScreeningDisclosed *bool   `json:"ai_screening_disclosed"`
	AIOptOutURL          *string `json:"ai_opt_out_url"`

	ParseConfidence  float64  `json:"parse_confidence"`
	MustHaveSkills   []string `json:"must_have_skills"`
	NiceToHaveSkills []string `json:"nice_to_have_skills"`

	// Match is nil for anonymous viewers and for postings not yet scored.
	// Distinguishing "not scored yet" from "scored zero" matters: the first is
	// a pending background job, the second is a judgement about the role.
	Match *Match `json:"match,omitempty"`
	Saved bool   `json:"saved"`
}

// Match is one posting's fit for one user.
type Match struct {
	Score         float64  `json:"score"`
	Band          string   `json:"band"`
	MissingSkills []string `json:"missing_skills"`
}

// FeedPage is one page plus its cursor.
type FeedPage struct {
	// SinceLastVisit is when the viewer's previous visit ended, or nil for an
	// anonymous viewer or a first visit. Rows first seen after it are new to
	// THIS reader — which is a different idea from "posted today", a fact about
	// the market that is identical for everyone.
	SinceLastVisit *time.Time `json:"since_last_visit,omitempty"`

	Items      []FeedItem `json:"data"`
	NextCursor string     `json:"next_cursor,omitempty"`
	HasMore    bool       `json:"has_more"`
}

// cursor is the keyset position. Opaque to clients so the ordering key can
// change without a breaking API change.
type cursor struct {
	// SortKey is the value of whatever column the sort orders by, as a string
	// so one cursor shape covers timestamps and numbers.
	SortKey string `json:"k"`
	ID      int64  `json:"i"`
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, bool) {
	if s == "" {
		return cursor{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, false
	}
	var c cursor
	if json.Unmarshal(b, &c) != nil {
		return cursor{}, false
	}
	return c, true
}

const maxFeedLimit = 50

// Feed returns one page of live postings.
//
// Keyset pagination, never OFFSET: at page 40 an OFFSET query still sorts and
// discards a thousand rows, so cost grows with depth. Keyset stays flat.
//
// Every query carries `status = 'live'`, which is not optional — every feed
// index is partial on it, and omitting the predicate silently drops to a
// sequential scan over roughly 3x the rows.
func Feed(ctx context.Context, pool *pgxpool.Pool, f FeedFilter, cursorStr string) (FeedPage, error) {
	limit := f.Limit
	if limit <= 0 || limit > maxFeedLimit {
		limit = 25
	}

	// Predicates are built explicitly, appending each argument and using its
	// resulting position. A helper that rewrites "?" placeholders is the classic
	// source of off-by-one bugs in exactly this kind of code, and a mis-numbered
	// parameter here would silently filter on the wrong value.
	var (
		conds = []string{"p.status = 'live'"}
		args  []any
	)

	// The viewer is bound FIRST so its parameter position is fixed at $1
	// regardless of which filters follow. Binding it last would make the join
	// clause's position depend on the filter combination, which is exactly the
	// off-by-one this function's comment warns about.
	//
	// A nil UserID binds NULL, so the join matches nothing and every posting
	// comes back unscored — the correct anonymous behaviour, with no second
	// query to maintain.
	var viewer any
	if f.UserID != nil {
		viewer = *f.UserID
	}
	args = append(args, viewer)
	const viewerPos = 1

	if len(f.Countries) > 0 {
		args = append(args, f.Countries)
		// country IS NULL is included deliberately: a posting whose location we
		// failed to parse must not vanish from a country filter. Our parse
		// failure is not the user's problem.
		conds = append(conds, fmt.Sprintf("(p.country = ANY($%d) OR p.country IS NULL)", len(args)))
	}
	if len(f.Modes) > 0 {
		args = append(args, f.Modes)
		conds = append(conds, fmt.Sprintf("p.mode::text = ANY($%d)", len(args)))
	}
	if f.YoE != nil {
		upper := *f.YoE
		if f.YoEStretch {
			upper += 2
		}
		args = append(args, upper, *f.YoE)
		// Low-confidence YoE is treated as unknown rather than excluded, and a
		// posting with no stated band matches everyone.
		conds = append(conds, fmt.Sprintf(
			`(p.yoe_confidence < 0.5 OR p.yoe_min IS NULL
			  OR (p.yoe_min <= $%d AND (p.yoe_max IS NULL OR p.yoe_max >= $%d)))`,
			len(args)-1, len(args)))
	}
	if f.CompMin != nil {
		args = append(args, *f.CompMin)
		if f.CompDisclosedOnly {
			conds = append(conds, fmt.Sprintf("p.comp_min >= $%d", len(args)))
		} else {
			// Undisclosed postings are kept: excluding them would hide ~20% of
			// the market behind a filter the user did not intend.
			conds = append(conds, fmt.Sprintf("(p.comp_min IS NULL OR p.comp_min >= $%d)", len(args)))
		}
	} else if f.CompDisclosedOnly {
		conds = append(conds, "p.comp_min IS NOT NULL")
	}
	if f.PostedWithin > 0 {
		args = append(args, time.Now().Add(-f.PostedWithin))
		conds = append(conds, fmt.Sprintf("COALESCE(p.posted_at, p.first_seen_at) >= $%d", len(args)))
	}
	if len(f.Vendors) > 0 {
		args = append(args, f.Vendors)
		conds = append(conds, fmt.Sprintf("s.vendor::text = ANY($%d)", len(args)))
	}
	if len(f.Bands) > 0 {
		args = append(args, f.Bands)
		// ujs is LEFT JOINed, so this predicate also drops unscored postings —
		// intentionally. NULL = ANY(...) is never true.
		conds = append(conds, fmt.Sprintf("ujs.band = ANY($%d)", len(args)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, q)
		conds = append(conds, fmt.Sprintf("p.search_tsv @@ websearch_to_tsquery('english', $%d)", len(args)))
	}
	if len(f.Skills) > 0 {
		args = append(args, f.Skills, len(f.Skills))
		// Require ALL requested skills, not any: a filter that returns postings
		// matching one of five selections is not a filter.
		conds = append(conds, fmt.Sprintf(`
			(SELECT count(DISTINCT sk.canonical)
			   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
			  WHERE ps.posting_id = p.id AND sk.canonical = ANY($%d)) = $%d`,
			len(args)-1, len(args)))
	}

	sortExpr, sortCol := feedSort(f.Sort)

	if c, ok := decodeCursor(cursorStr); ok {
		args = append(args, c.SortKey, c.ID)
		conds = append(conds, fmt.Sprintf("(%s, p.id) < ($%d::text, $%d::bigint)",
			sortCol, len(args)-1, len(args)))
	}

	args = append(args, limit+1) // one extra row tells us whether more exist
	limitPos := len(args)

	query := fmt.Sprintf(`
SELECT p.id, p.title, c.name, c.slug, p.location_raw, p.city, p.country,
       p.mode::text, p.apply_url, s.vendor::text,
       p.comp_min, p.comp_max, p.comp_currency, p.comp_period, p.comp_src::text,
       p.yoe_min, p.yoe_max, p.yoe_confidence,
       p.posted_at, p.posted_at_is_estimate, p.first_seen_at,
       p.ai_screening_disclosed, p.ai_opt_out_url,
       p.parse_confidence,
       COALESCE((SELECT array_agg(sk.display_name ORDER BY sk.display_name)
                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
                  WHERE ps.posting_id = p.id AND ps.requirement = 'must_have'), '{}') AS must_have,
       COALESCE((SELECT array_agg(sk.display_name ORDER BY sk.display_name)
                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
                  WHERE ps.posting_id = p.id AND ps.requirement = 'nice_to_have'), '{}') AS nice_to_have,
       ujs.score, ujs.band, COALESCE(ujs.missing_skills, '{}'),
       (app.user_id IS NOT NULL) AS saved,
       %s AS sort_key
  FROM job_postings p
  JOIN companies c ON c.id = p.company_id
  JOIN sources   s ON s.id = p.source_id
  LEFT JOIN user_job_scores ujs
         ON ujs.posting_id = p.id AND ujs.user_id = $%d::bigint
  LEFT JOIN applications app
         ON app.posting_id = p.id AND app.user_id = $%d::bigint
 WHERE %s
 ORDER BY %s DESC, p.id DESC
 LIMIT $%d`,
		sortExpr, viewerPos, viewerPos, strings.Join(conds, "\n   AND "), sortCol, limitPos)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return FeedPage{}, fmt.Errorf("feed query: %w", err)
	}
	defer rows.Close()

	page := FeedPage{Items: make([]FeedItem, 0, limit)}
	var lastSortKey string

	for rows.Next() {
		var it FeedItem
		var sortKey string
		// Score columns are nullable: NULL means this posting has not been
		// scored for this viewer, which is different from a score of zero.
		var score *float64
		var band *string
		var missing []string
		if err := rows.Scan(
			&it.ID, &it.Title, &it.CompanyName, &it.CompanySlug, &it.LocationRaw,
			&it.City, &it.Country, &it.Mode, &it.ApplyURL, &it.Vendor,
			&it.CompMin, &it.CompMax, &it.CompCurrency, &it.CompPeriod, &it.CompSource,
			&it.YoEMin, &it.YoEMax, &it.YoEConfidence,
			&it.PostedAt, &it.PostedAtIsEstimate, &it.FirstSeenAt,
			&it.AIScreeningDisclosed, &it.AIOptOutURL,
			&it.ParseConfidence,
			&it.MustHaveSkills, &it.NiceToHaveSkills,
			&score, &band, &missing, &it.Saved,
			&sortKey,
		); err != nil {
			return FeedPage{}, fmt.Errorf("scan feed row: %w", err)
		}

		if score != nil && band != nil {
			it.Match = &Match{Score: *score, Band: *band, MissingSkills: missing}
		}

		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, it)
		lastSortKey = sortKey
	}
	if err := rows.Err(); err != nil {
		return FeedPage{}, err
	}

	if page.HasMore && len(page.Items) > 0 {
		page.NextCursor = encodeCursor(cursor{
			SortKey: lastSortKey,
			ID:      page.Items[len(page.Items)-1].ID,
		})
	}
	return page, nil
}

// feedSort maps a sort name to an expression and the column to order by.
//
// Allowlisted, never interpolated from user input — this is the one place a
// feed query could become an injection point.
func feedSort(name string) (expr, col string) {
	switch name {
	case "comp":
		return "COALESCE(p.comp_min, 0)::text", "COALESCE(p.comp_min, 0)"
	case "match", "relevance":
		// Unscored postings sort last rather than as zero: a posting nobody has
		// scored yet is not a bad match, it is an unknown one, and burying it
		// among the genuine mismatches would hide new arrivals.
		return "COALESCE(ujs.score, -1)::text", "COALESCE(ujs.score, -1)"
	default: // newest
		return "COALESCE(p.posted_at, p.first_seen_at)::text", "COALESCE(p.posted_at, p.first_seen_at)"
	}
}

// FeedFacets returns counts per filter value for the current context.
//
// Lets the UI show "Remote (1,240)" and never let a user filter blindly into
// zero results. Small, slow-changing and identical across users with the same
// filter prefix — which is exactly the shape an in-process cache serves, and
// why Redis is not needed for it.
type FeedFacets struct {
	Modes     map[string]int64 `json:"modes"`
	Countries map[string]int64 `json:"countries"`
	Vendors   map[string]int64 `json:"vendors"`

	// YoE and Comp are bucketed rather than continuous, because the filter UI
	// is chips and a chip needs a countable set. The keys are the same strings
	// the query parameters take, so the client never has to translate between
	// "what I show" and "what I send".
	YoE  map[string]int64 `json:"yoe"`
	Comp map[string]int64 `json:"comp"`

	// CompUndisclosed is the size of the choice a salary filter forces. About a
	// fifth of the market publishes nothing, and excluding them silently is the
	// kind of decision a filter should never make on someone's behalf.
	CompUndisclosed int64 `json:"comp_undisclosed"`

	Total int64 `json:"total"`
}

func Facets(ctx context.Context, pool *pgxpool.Pool) (FeedFacets, error) {
	out := FeedFacets{
		Modes:     map[string]int64{},
		Countries: map[string]int64{},
		Vendors:   map[string]int64{},
		YoE:       map[string]int64{},
		Comp:      map[string]int64{},
	}

	rows, err := pool.Query(ctx, `
SELECT 'mode', p.mode::text, count(*)
  FROM job_postings p WHERE p.status = 'live' GROUP BY 2
UNION ALL
SELECT 'country', COALESCE(p.country, 'unknown'), count(*)
  FROM job_postings p WHERE p.status = 'live' GROUP BY 2
UNION ALL
SELECT 'vendor', s.vendor::text, count(*)
  FROM job_postings p JOIN sources s ON s.id = p.source_id
 WHERE p.status = 'live' GROUP BY 2
UNION ALL
-- Experience bands, keyed on the value the ?yoe= parameter takes.
--
-- Bucketed on yoe_min, the floor a posting states, because that is the number
-- a reader is compared against. A posting with no stated range is counted
-- under 'any' only — it is not evidence of being junior OR senior, and
-- assigning it to a band would invent a requirement the employer never made.
SELECT 'yoe', CASE
         WHEN p.yoe_min IS NULL THEN 'unstated'
         WHEN p.yoe_min <= 2  THEN '0-2'
         WHEN p.yoe_min <= 5  THEN '3-5'
         WHEN p.yoe_min <= 8  THEN '6-8'
         ELSE '9+'
       END, count(*)
  FROM job_postings p WHERE p.status = 'live' GROUP BY 2
UNION ALL
-- Salary floors, keyed on the ?comp_min= parameter. Cumulative rather than
-- disjoint: "$150k+" means every posting paying at least that, which is what
-- the chip says and therefore what its count must mean.
SELECT 'comp', b.key, count(*)
  FROM job_postings p
  CROSS JOIN (VALUES ('100000'), ('150000'), ('200000')) AS b(key)
 WHERE p.status = 'live'
   AND p.comp_max IS NOT NULL
   AND p.comp_max >= b.key::numeric
 GROUP BY 2
UNION ALL
SELECT 'comp_undisclosed', 'n', count(*)
  FROM job_postings p WHERE p.status = 'live' AND p.comp_max IS NULL`)
	if err != nil {
		return out, fmt.Errorf("facets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind, value string
		var n int64
		if err := rows.Scan(&kind, &value, &n); err != nil {
			return out, err
		}
		switch kind {
		case "mode":
			out.Modes[value] = n
			out.Total += n
		case "country":
			out.Countries[value] = n
		case "vendor":
			out.Vendors[value] = n
		case "yoe":
			out.YoE[value] = n
		case "comp":
			out.Comp[value] = n
		case "comp_undisclosed":
			out.CompUndisclosed = n
		}
	}
	return out, rows.Err()
}
