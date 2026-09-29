package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/matching"
)

// FeedFilter is the set of constraints from the query string.
//
// Every field is optional. An unset filter must never exclude anything — the
// governing rule is that our uncertainty never becomes the user's penalty.
type FeedFilter struct {
	Countries []string
	Modes     []string

	// YoEBands are experience bands, keyed exactly as the facet buckets and the
	// chips: "0-2", "3-5", "6-8", "9+". Multi-select, because "0-2 or 3-5" is
	// one question.
	//
	// It used to be a single threshold with an invisible +2 stretch, and the
	// three parts of the control disagreed: the chip was LABELLED by band,
	// COUNTED by band, and FILTERED by threshold-plus-stretch. Selecting
	// "0-2 yrs 835" returned postings asking for 3, 4 and 3-5 years. A control
	// whose own count contradicts its result is worse than no control.
	YoEBands []string

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

	// Field narrows to a kind of work. Empty means the DEFAULT, which is not
	// "everything" — it hides `other` and keeps `software` and `unknown`. See
	// addField for why that asymmetry is the honest one.
	Field string

	Sort  string // newest | comp | match
	Limit int

	// UserID scopes match scores to one viewer. nil for anonymous requests,
	// where the score columns come back empty rather than absent — the feed is
	// public, and gating discovery behind an account would be the wrong trade.
	UserID *int64

	// IncludeDismissed shows hidden postings anyway, for the review list. The
	// default is to hide them, so a caller that forgets this field gets the
	// behaviour the user asked for rather than the one they did not.
	IncludeDismissed bool
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

	// scoring holds what the scorer needs, carried from the row rather than
	// fetched again. Unexported so it cannot reach the wire: it is an input to
	// Match, not a field of the response.
	scoring matching.Posting
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
	// Offset is used only by the ranked path, where the sort column is computed
	// in Go and there is no stable column for a keyset. Absent from every
	// cursor the keyset path issues, so old cursors decode with Offset 0.
	Offset int `json:"o,omitempty"`
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
// feedPredicates accumulates WHERE clauses and the arguments they bind.
//
// Positions are assigned by bind() rather than by rewriting "?" placeholders. A
// helper that renumbers placeholders is the classic source of off-by-one bugs
// in exactly this kind of code, and a mis-numbered parameter here would
// silently filter on the wrong value rather than fail.
type feedPredicates struct {
	// queryPos is where the search text was bound, or 0. The relevance sort
	// ranks on the same parameter rather than binding it twice — two copies of
	// one string is two things that can drift.
	queryPos int

	conds []string
	args  []any
}

// newFeedPredicates binds the viewer FIRST, fixing its position at $1
// regardless of which filters follow. Binding it last would make the join
// clause's position depend on the filter combination, which is exactly the
// off-by-one bind() exists to prevent.
//
// A nil userID binds NULL, so the join matches nothing and every posting comes
// back unscored — the correct anonymous behaviour, with no second query to
// maintain.
func newFeedPredicates(userID *int64) *feedPredicates {
	var viewer any
	if userID != nil {
		viewer = *userID
	}
	return &feedPredicates{
		conds: []string{"p.status = 'live'"},
		args:  []any{viewer},
	}
}

const viewerPos = 1

// newCandidatePredicates builds the same filters WITHOUT binding a viewer.
//
// The candidate query joins neither user_job_scores nor applications, so a
// bound-but-unused $1 leaves Postgres unable to infer its type — "could not
// determine data type of parameter $1". Positions are assigned by bind()
// rather than hardcoded, so dropping the viewer simply shifts every filter
// down by one and nothing else has to know.
func newCandidatePredicates() *feedPredicates {
	return &feedPredicates{conds: []string{"p.status = 'live'"}}
}

// bind appends an argument and returns its 1-based position.
func (p *feedPredicates) bind(v any) int {
	p.args = append(p.args, v)
	return len(p.args)
}

func (p *feedPredicates) where(format string, pos ...any) {
	p.conds = append(p.conds, fmt.Sprintf(format, pos...))
}

func (p *feedPredicates) addLocation(f FeedFilter) {
	if len(f.Countries) > 0 {
		// country IS NULL is included deliberately: a posting whose location we
		// failed to parse must not vanish from a country filter. Our parse
		// failure is not the user's problem.
		p.where("(p.country = ANY($%d) OR p.country IS NULL)", p.bind(f.Countries))
	}
	if len(f.Modes) > 0 {
		p.where("p.mode::text = ANY($%d)", p.bind(f.Modes))
	}
}

// yoeBandSQL is the band test, keyed identically to the facet buckets in
// facetsSQL. The two must agree by construction: the number on a chip and the
// rows behind it are the same claim.
var yoeBandSQL = map[string]string{
	"0-2": "p.yoe_min <= 2",
	"3-5": "p.yoe_min BETWEEN 3 AND 5",
	"6-8": "p.yoe_min BETWEEN 6 AND 8",
	"9+":  "p.yoe_min >= 9",
}

func (p *feedPredicates) addExperience(f FeedFilter) {
	if len(f.YoEBands) == 0 {
		return
	}
	bands := make([]string, 0, len(f.YoEBands))
	for _, b := range f.YoEBands {
		if sql, ok := yoeBandSQL[b]; ok {
			bands = append(bands, sql)
		}
	}
	if len(bands) == 0 {
		return
	}
	// A posting that states no range, or one we read with low confidence, is
	// never filtered out — our failure to read a requirement must not hide the
	// job. The rail says so beside the chips.
	p.where("(p.yoe_confidence < 0.5 OR p.yoe_min IS NULL OR (%s))",
		strings.Join(bands, " OR "))
}

// addCompensation filters on VALUE, not on the size of the number.
//
// The chips say "$100k", "$150k", "$200k" — a dollar figure — and the predicate
// compared them against comp_min in the posting's OWN currency. A "$200k+"
// filter therefore returned a 3,575,300 INR role worth $37,555 and an
// 11,950,000 JPY role worth $74,787, because both are numerically larger than
// 200000. Eight such postings were in the corpus.
//
// The conversion is the same expression the salary sort uses, for the same
// reason and with the same caveat: the rates decide COMPARISONS, never what is
// displayed. The figure a reader sees is always the employer's own.
func (p *feedPredicates) addCompensation(f FeedFilter) {
	switch {
	case f.CompMin != nil && f.CompDisclosedOnly:
		p.where(compInUSD+" >= $%d", p.bind(*f.CompMin))
	case f.CompMin != nil:
		// Undisclosed postings are kept: excluding them would hide a fifth of
		// the market behind a filter the user did not intend. COALESCE makes an
		// undisclosed salary 0, so the IS NULL test is what keeps them — not an
		// accident of the arithmetic.
		p.where("(p.comp_min IS NULL OR "+compInUSD+" >= $%d)", p.bind(*f.CompMin))
	case f.CompDisclosedOnly:
		p.where("p.comp_min IS NOT NULL")
	}
}

// addDismissed hides what the viewer has already rejected.
//
// NOT EXISTS rather than a LEFT JOIN ... IS NULL: the planner turns it into an
// anti-join either way, but NOT EXISTS cannot accidentally multiply rows if the
// key ever stops being unique, and it reads as the question being asked.
//
// Anonymous viewers have no dismissals, so the predicate is omitted entirely
// rather than bound to NULL — an unnecessary subquery on every anonymous feed
// request is the most-served query in the product.
// addField hides work that is not what this product is for.
//
// The default is `field <> 'other'`, NOT `field = 'software'`, and the
// difference is the whole design. Two-thirds of the corpus is not software
// engineering, but the classifier can only name a third of it with confidence —
// the rest is a multilingual long tail from one conglomerate's board. Defaulting
// to `= 'software'` would hide every posting we failed to classify, including
// the software ones.
//
// So the two ways of being wrong are priced differently: a non-software posting
// left in the feed costs the reader one row they can see is wrong, and a
// software posting excluded costs them a job they will never know existed.
//
// `all` is a real option rather than a hidden one, because a reader who
// disagrees with the classifier has to be able to overrule it.
func (p *feedPredicates) addField(f FeedFilter) {
	switch f.Field {
	case "all":
		return
	case "software", "other", "unknown":
		p.where("p.field = $%d", p.bind(f.Field))
	default:
		p.where("p.field <> 'other'")
	}
}

func (p *feedPredicates) addDismissed(f FeedFilter) {
	if f.UserID == nil || f.IncludeDismissed {
		return
	}
	p.where(`NOT EXISTS (SELECT 1 FROM dismissed_postings d
	                      WHERE d.user_id = $%d AND d.posting_id = p.id)`, p.bind(*f.UserID))
}

func (p *feedPredicates) addSource(f FeedFilter) {
	if f.PostedWithin > 0 {
		p.where("COALESCE(p.posted_at, p.first_seen_at) >= $%d",
			p.bind(time.Now().Add(-f.PostedWithin)))
	}
	if len(f.Vendors) > 0 {
		p.where("s.vendor::text = ANY($%d)", p.bind(f.Vendors))
	}
}

func (p *feedPredicates) addSearch(f FeedFilter) {
	if q := strings.TrimSpace(f.Query); q != "" {
		p.queryPos = p.bind(q)
		p.where("p.search_tsv @@ websearch_to_tsquery('english', $%d)", p.queryPos)
	}
	if len(f.Skills) > 0 {
		// Require ALL requested skills, not any: a filter that returns postings
		// matching one of five selections is not a filter.
		p.where(`
			(SELECT count(DISTINCT sk.canonical)
			   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
			  WHERE ps.posting_id = p.id AND sk.canonical = ANY($%d)) = $%d`,
			p.bind(f.Skills), p.bind(len(f.Skills)))
	}
}

func (p *feedPredicates) addCursor(cursorStr string, sort sortMode) {
	c, ok := decodeCursor(cursorStr)
	if !ok {
		return
	}
	// The cursor value is cast to the sort column's own type, not the column to
	// text. Casting the column to text made this comparison `timestamptz <
	// text`, which Postgres rejects outright — so keyset pagination returned
	// 500 for every sort mode. See TestFeedCursorPagination.
	p.where("(%s, p.id) < ($%d::%s, $%d::bigint)",
		sort.col, p.bind(c.SortKey), sort.cursorCast, p.bind(c.ID))
}

// feedSelect is the projection, held apart from the predicates so the two are
// read separately — the columns almost never change and the filters often do.
const feedSelect = `
SELECT p.id, p.title, c.name, c.slug,
       -- location_raw is nullable and FeedItem.LocationRaw is a plain string,
       -- so a NULL fails the scan and takes the whole feed with it. No live row
       -- is NULL today, but a remote-only posting with no stated location is an
       -- ordinary thing for an adapter to produce. The detail query already
       -- COALESCEs this; the feed did not.
       COALESCE(p.location_raw, '') AS location_raw,
       p.city, p.country,
       p.mode::text, p.apply_url, s.vendor::text,
       p.comp_min, p.comp_max, p.comp_currency, p.comp_period, p.comp_src::text,
       p.yoe_min, p.yoe_max,
       -- Same nullable-into-plain-float64 hazard as location_raw above, and
       -- the only other one in this projection: every remaining non-pointer
       -- field maps to a NOT NULL column. Zero means "we know nothing about
       -- the range", which is what NULL means here.
       COALESCE(p.yoe_confidence, 0) AS yoe_confidence,
       p.posted_at, p.posted_at_is_estimate, p.first_seen_at,
       p.ai_screening_disclosed, p.ai_opt_out_url,
       p.parse_confidence,
       COALESCE((SELECT array_agg(sk.display_name ORDER BY sk.display_name)
                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
                  WHERE ps.posting_id = p.id AND ps.requirement = 'must_have'), '{}') AS must_have,
       COALESCE((SELECT array_agg(sk.display_name ORDER BY sk.display_name)
                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
                  WHERE ps.posting_id = p.id AND ps.requirement = 'nice_to_have'), '{}') AS nice_to_have,
       -- Canonical skill names, for the scorer. The two arrays above are
       -- display names, for the chips: a user reads "PostgreSQL", the model
       -- matches "postgresql", and conflating them once put "node.js" on a
       -- chip. Both are carried because scoring now happens here rather than
       -- being read back from a table.
       COALESCE((SELECT array_agg(sk.canonical)
                   FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
                  WHERE ps.posting_id = p.id AND ps.requirement = 'must_have'), '{}') AS must_canon,
       COALESCE((
         SELECT array_agg(sk.canonical)
           FROM posting_skills ps JOIN skills sk ON sk.id = ps.skill_id
          WHERE ps.posting_id = p.id
            AND ps.requirement = CASE
                  WHEN EXISTS (
                    SELECT 1 FROM posting_skills q
                     WHERE q.posting_id = p.id
                       AND q.requirement IN ('must_have','nice_to_have')
                  ) THEN 'nice_to_have'::skill_requirement
                  ELSE 'mentioned'::skill_requirement
                END
       ), '{}') AS nice_canon,
       (app.user_id IS NOT NULL) AS saved,
       %s AS sort_key
  FROM job_postings p
  JOIN companies c ON c.id = p.company_id
  JOIN sources   s ON s.id = p.source_id
  LEFT JOIN applications app
         ON app.posting_id = p.id AND app.user_id = $%d::bigint
 WHERE %s
 ORDER BY %s DESC, p.id DESC
 LIMIT $%d`

// scoreCandidateCap bounds the set feedRanked scores in one request.
//
// 2,000, and the number is measured rather than reasoned. ADR-0016 first set it
// at 20,000 from the scoring cost alone — 20,000 x 1.84 us = 37 ms — which was
// the wrong constraint. Reading the candidates dominates: the projection costs
// about 21 us per row, so the query, not the scorer, is what spends the budget.
//
//	   500 candidates ->  15 ms
//	 1,000            ->  24 ms
//	 2,000            ->  45 ms
//	 5,000            -> 108 ms
//	12,000            -> 263 ms
//
// At 2,000 the whole request is roughly 45 ms of candidates plus 4 ms of
// scoring plus the page fetch, inside a 120 ms budget. At 5,000 it is already
// over.
//
// Filters apply BEFORE the cap, which is what makes this acceptable: a reader
// who narrows to remote, or to India, or to a skill, has a candidate set well
// under the cap and gets their whole result ranked. The cap only binds an
// unfiltered "sort everything by match", where the honest answer is that the
// newest 2,000 is the part worth ranking anyway.
const scoreCandidateCap = 2_000

// needsRanking reports whether this request cannot be answered without scores.
//
// "relevance" is deliberately NOT here. It ranks by the text index, which is a
// SQL ordering the keyset path can do — routing it through the scorer would
// rank search results by profile fit rather than by what the reader typed.
func needsRanking(f FeedFilter) bool {
	if f.Sort == "match" {
		return true
	}
	return len(f.Bands) > 0
}

// Feed returns one page of postings matching a filter, scored for the viewer.
//
// Scores are computed here, not read: ADR-0016. Two paths, because they have
// different costs and only one of them needs the whole candidate set.
//
//   - Ordering that does not depend on a score (newest, comp) keeps keyset
//     pagination and scores only the page it returns — 25 rows, ~46 us.
//   - Ordering or filtering that DOES depend on a score has to score the whole
//     candidate set before it can rank it, so it takes the bounded path.
func Feed(
	ctx context.Context,
	pool *pgxpool.Pool,
	scorer *matching.Scorer,
	f FeedFilter,
	cursorStr string,
) (FeedPage, error) {
	var profile *matching.Profile
	if f.UserID != nil {
		p, err := ProfileForScoring(ctx, pool, *f.UserID)
		if err != nil {
			return FeedPage{}, fmt.Errorf("load viewer profile: %w", err)
		}
		profile = &p
	}

	if needsRanking(f) {
		if profile == nil {
			// A band is a statement about one person's fit. Rejected rather
			// than silently ignored, which would return an unfiltered feed that
			// looks like the filter did nothing.
			return FeedPage{}, ErrRankingNeedsViewer
		}
		return feedRanked(ctx, pool, scorer, f, cursorStr, *profile)
	}
	return feedKeyset(ctx, pool, scorer, f, cursorStr, profile)
}

// feedKeyset is the score-independent path: keyset pagination, unchanged, with
// the returned page scored on the way out.
func feedKeyset(
	ctx context.Context,
	pool *pgxpool.Pool,
	scorer *matching.Scorer,
	f FeedFilter,
	cursorStr string,
	profile *matching.Profile,
) (FeedPage, error) {
	limit := f.Limit
	if limit <= 0 || limit > maxFeedLimit {
		limit = 25
	}

	p := newFeedPredicates(f.UserID)
	p.addLocation(f)
	p.addExperience(f)
	p.addCompensation(f)
	p.addSource(f)
	p.addSearch(f)
	p.addField(f)
	p.addDismissed(f)

	// After addSearch, because relevance ranks on the parameter it bound. The
	// cursor then compares the same expression, so paging a ranked list stays
	// consistent.
	sort := feedSort(f.Sort, p.queryPos)
	p.addCursor(cursorStr, sort)

	// One extra row is what tells us whether another page exists.
	limitPos := p.bind(limit + 1)

	query := fmt.Sprintf(feedSelect, sort.expr, viewerPos,
		strings.Join(p.conds, "\n   AND "), sort.col, limitPos)

	rows, err := pool.Query(ctx, query, p.args...)
	if err != nil {
		return FeedPage{}, fmt.Errorf("feed query: %w", err)
	}
	defer rows.Close()

	page, err := collectFeedPage(rows, limit)
	if err != nil {
		return page, err
	}
	if profile != nil {
		for i := range page.Items {
			page.Items[i].Match = scoreItem(scorer, *profile, page.Items[i])
		}
	}
	return page, nil
}

// scoreItem runs the scorer for one posting and shapes the wire form.
func scoreItem(scorer *matching.Scorer, profile matching.Profile, it FeedItem) *Match {
	r := scorer.Score(profile, it.scoring)
	return &Match{
		Score:         r.Score,
		Band:          string(r.Band),
		MissingSkills: r.MissingSkills(),
	}
}

// ErrRankingNeedsViewer is returned when a request asks for an ordering or a
// filter that only means something for a specific person, without one.
var ErrRankingNeedsViewer = errors.New("store: ranking requires a signed-in viewer")

// feedRanked is the score-dependent path.
//
// Everything in the candidate set has to be scored before any of it can be
// ranked, so this reads a bounded set ordered by recency, scores it, and pages
// within the result. Recency is the right bound: a role from four months ago
// that scores 91 is not more useful than a fresh one that scores 88, and
// bounding by anything else would mean ranking a set that grows forever.
//
// Paging is by offset here, not keyset, and that is a deliberate exception. A
// keyset cursor needs a stable sort column in SQL; the sort column is computed
// in Go. The offset is safe because it walks a set that is bounded and
// deterministically re-derived — the same filters produce the same candidates
// and the scorer is a pure function, so page 2 is the same ranking page 1 came
// from.
func feedRanked(
	ctx context.Context,
	pool *pgxpool.Pool,
	scorer *matching.Scorer,
	f FeedFilter,
	cursorStr string,
	profile matching.Profile,
) (FeedPage, error) {
	limit := f.Limit
	if limit <= 0 || limit > maxFeedLimit {
		limit = 25
	}

	ranked, err := rankCandidates(ctx, pool, scorer, f, profile)
	if err != nil {
		return FeedPage{}, err
	}

	offset := 0
	if c, ok := decodeCursor(cursorStr); ok {
		offset = c.Offset
	}
	offset = min(offset, len(ranked))
	end := min(offset+limit, len(ranked))

	page := FeedPage{Items: []FeedItem{}, HasMore: end < len(ranked)}
	if page.HasMore {
		page.NextCursor = encodeCursor(cursor{Offset: end})
	}
	if offset == end {
		return page, nil
	}

	// Full rows for the page only. This is the whole reason ranking reads a
	// lean projection first: the display query carries four correlated
	// subqueries and a company join, and running it over 12,000 candidates
	// measured 1.3 SECONDS against a 120 ms budget. Scoring was never the cost
	// — it is 22 ms for the same set — the cost was fetching rows nobody would
	// see. Twenty-five of them is 40 ms.
	page.Items, err = feedItemsByID(ctx, pool, f.UserID, ranked[offset:end])
	if err != nil {
		return FeedPage{}, err
	}
	return page, nil
}

// rankedPosting is one scored candidate, before its display row is fetched.
type rankedPosting struct {
	id    int64
	match Match
	// posted breaks ties, so the order is total. Without it the order for equal
	// scores depends on the query plan, and a page boundary landing inside a
	// tie would drop or repeat a posting between pages.
	posted time.Time
}

// rankCandidates reads the bounded candidate set through the lean scoring
// projection, scores it, and returns it ranked.
func rankCandidates(
	ctx context.Context,
	pool *pgxpool.Pool,
	scorer *matching.Scorer,
	f FeedFilter,
	profile matching.Profile,
) ([]rankedPosting, error) {
	p := newCandidatePredicates()
	p.addLocation(f)
	p.addExperience(f)
	p.addCompensation(f)
	p.addSource(f)
	p.addSearch(f)
	p.addField(f)
	p.addDismissed(f)
	capPos := p.bind(scoreCandidateCap)

	// Concatenated, not Sprintf'd. postingScoringColumns carries a SQL comment
	// containing "73.7%", and a stray % in a format string is a verb — it
	// compiled into a query with "% o" in it before go vet caught it.
	query := "SELECT " + postingScoringColumns + `
		  FROM job_postings p
		  JOIN sources s ON s.id = p.source_id
		 WHERE ` + strings.Join(p.conds, "\n   AND ") + fmt.Sprintf(`
		 ORDER BY COALESCE(p.posted_at, p.first_seen_at) DESC, p.id DESC
		 LIMIT $%d`, capPos)

	rows, err := pool.Query(ctx, query, p.args...)
	if err != nil {
		return nil, fmt.Errorf("feed candidates: %w", err)
	}
	defer rows.Close()

	want := bandSet(f.Bands)
	var out []rankedPosting
	for rows.Next() {
		j, err := scanPosting(rows)
		if err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		r := scorer.Score(profile, j)
		band := string(r.Band)
		if want != nil && !want[band] {
			continue
		}
		out = append(out, rankedPosting{
			id:     j.ID,
			match:  Match{Score: r.Score, Band: band, MissingSkills: r.MissingSkills()},
			posted: j.PostedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(out, func(i, k int) bool {
		if out[i].match.Score != out[k].match.Score {
			return out[i].match.Score > out[k].match.Score
		}
		return out[i].posted.After(out[k].posted)
	})
	return out, nil
}

// feedItemsByID fetches display rows for an already-ranked page, preserving the
// ranking. Postgres returns no order for `id = ANY(...)`, so the order is
// restored here rather than asked for in SQL.
func feedItemsByID(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID *int64,
	ranked []rankedPosting,
) ([]FeedItem, error) {
	ids := make([]int64, len(ranked))
	for i, r := range ranked {
		ids[i] = r.id
	}

	var viewer any
	if userID != nil {
		viewer = *userID
	}
	sortMode := feedSort("newest", 0)
	query := fmt.Sprintf(feedSelect, sortMode.expr, viewerPos, "p.id = ANY($2)", sortMode.col, 3)

	rows, err := pool.Query(ctx, query, viewer, ids, len(ids))
	if err != nil {
		return nil, fmt.Errorf("feed page rows: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]FeedItem, len(ids))
	for rows.Next() {
		it, _, err := scanFeedItem(rows)
		if err != nil {
			return nil, err
		}
		byID[it.ID] = it
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]FeedItem, 0, len(ranked))
	for _, r := range ranked {
		it, ok := byID[r.id]
		if !ok {
			// Closed between the two queries. Dropping it is correct: the feed
			// only ever shows live postings.
			continue
		}
		m := r.match
		it.Match = &m
		out = append(out, it)
	}
	return out, nil
}

func bandSet(bands []string) map[string]bool {
	if len(bands) == 0 {
		return nil
	}
	out := make(map[string]bool, len(bands))
	for _, b := range bands {
		out[b] = true
	}
	return out
}

// collectFeedPage reads rows into a page and derives the next cursor.
func collectFeedPage(rows pgx.Rows, limit int) (FeedPage, error) {
	page := FeedPage{Items: make([]FeedItem, 0, limit)}
	var lastSortKey string

	for rows.Next() {
		it, sortKey, err := scanFeedItem(rows)
		if err != nil {
			return FeedPage{}, err
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

func scanFeedItem(rows pgx.Rows) (FeedItem, string, error) {
	var it FeedItem
	var sortKey string
	var mustCanon, niceCanon []string

	if err := rows.Scan(
		&it.ID, &it.Title, &it.CompanyName, &it.CompanySlug, &it.LocationRaw,
		&it.City, &it.Country, &it.Mode, &it.ApplyURL, &it.Vendor,
		&it.CompMin, &it.CompMax, &it.CompCurrency, &it.CompPeriod, &it.CompSource,
		&it.YoEMin, &it.YoEMax, &it.YoEConfidence,
		&it.PostedAt, &it.PostedAtIsEstimate, &it.FirstSeenAt,
		&it.AIScreeningDisclosed, &it.AIOptOutURL,
		&it.ParseConfidence,
		&it.MustHaveSkills, &it.NiceToHaveSkills,
		&mustCanon, &niceCanon, &it.Saved,
		&sortKey,
	); err != nil {
		return it, "", fmt.Errorf("scan feed row: %w", err)
	}

	it.scoring = matching.Posting{
		ID: it.ID, Country: derefString(it.Country), Mode: it.Mode,
		YoEMin: it.YoEMin, YoEMax: it.YoEMax, YoEConfidence: it.YoEConfidence,
		CompMin: it.CompMin, CompMax: it.CompMax,
		CompCurrency:    derefString(it.CompCurrency),
		ParseConfidence: it.ParseConfidence,
		MustHaveSkills:  mustCanon, NiceToHaveSkills: niceCanon,
	}
	if it.PostedAt != nil {
		it.scoring.PostedAt = *it.PostedAt
	} else {
		it.scoring.PostedAt = it.FirstSeenAt
	}
	return it, sortKey, nil
}

// sortMode is one allowlisted ordering.
//
// cursorCast is the type the stored cursor value is cast BACK to when
// comparing. It is not decoration: the cursor is carried as text because it
// travels in a URL, but comparing a timestamp column against a text parameter
// is an error in Postgres, and comparing a numeric one against text is worse —
// it succeeds and orders lexicographically, so 9 sorts after 100.
type sortMode struct {
	// expr is the projected sort_key, always cast to text so one cursor format
	// serves every mode.
	expr string
	// col is the bare expression to ORDER BY, in its own type.
	col string
	// cursorCast is col's type, for the keyset comparison.
	cursorCast string
}

// feedSort maps a sort name to an expression and the column to order by.
//
// Allowlisted, never interpolated from user input — this is the one place a
// feed query could become an injection point.
// compInUSD orders salaries across currencies.
//
// Sorting on the raw figure is wrong and visibly so: 13,750,000 JPY is about
// $92,000 and outranked every $850,000 role in the corpus, purely because yen
// are numerous. A reader sorting by salary got the currencies with the largest
// numbers, not the best-paid jobs.
//
// THE RATES ORDER, THEY NEVER DISPLAY. The figure shown to a reader is always
// the employer's own, in the employer's own currency; this expression exists
// only to decide which row comes first. That distinction is what makes a
// hardcoded rate acceptable here — a stale rate shuffles two similar salaries,
// where a stale rate on a DISPLAYED figure would be a fabricated number, which
// this product may not do.
//
// Mid-market rates against USD, exchangerate-api.com, 2026-09-01. Nine
// currencies because nine are what the corpus contains; a rate for a currency
// nobody posts in is a line nobody can verify. An unlisted currency falls
// through to 1.0 and sorts on its raw figure, which is the old behaviour and no
// worse than it was.
//
// Drift is tolerable by construction. These move a few percent a year, and a
// few percent only reorders salaries that were already neighbours.
// Parenthesised: the cursor appends ::text to this expression, and ::text binds
// tighter than /, so an unwrapped version parsed as numeric / (CASE...)::text
// and the feed 500'd with "operator does not exist: numeric / text".
const compInUSD = `(COALESCE(p.comp_min, 0) / CASE p.comp_currency
	WHEN 'GBP' THEN 0.738359
	WHEN 'EUR' THEN 0.861401
	WHEN 'CAD' THEN 1.386295
	WHEN 'SGD' THEN 1.271737
	WHEN 'AUD' THEN 1.395860
	WHEN 'JPY' THEN 159.787727
	WHEN 'SEK' THEN 9.580693
	WHEN 'INR' THEN 95.201365
	ELSE 1.0
END)`

func feedSort(name string, queryPos int) sortMode {
	// Recency is both the default and the fallback, so it is named once.
	newest := sortMode{
		"COALESCE(p.posted_at, p.first_seen_at)::text",
		"COALESCE(p.posted_at, p.first_seen_at)",
		"timestamptz",
	}

	switch name {
	case "comp":
		return sortMode{compInUSD + "::text", compInUSD, "numeric"}

	case "relevance":
		// Rank by the text index. The tsvector weights the title 'A' and the
		// body 'B', so a title hit outranks a passing mention in a description —
		// which is exactly why searching "software engineer 1" returned
		// "Enterprise Customer Engineer, Vietnam" first. websearch_to_tsquery
		// ANDs the terms, so that posting genuinely contained all three
		// somewhere in its body; nothing ranked it below a title match, and the
		// list came back in date order.
		//
		// Relevance to nothing is not an ordering, so with no query this falls
		// back to recency rather than ranking every row equally and shuffling.
		if queryPos == 0 {
			return newest
		}
		rank := fmt.Sprintf("ts_rank(p.search_tsv, websearch_to_tsquery('english', $%d))", queryPos)
		return sortMode{rank + "::text", rank, "real"}

	case "match":
		// Handled entirely in Go by feedRanked — there is no score column to
		// order by any more. SQL still orders the CANDIDATE set by recency,
		// which is what this returns.
		return newest
	}
	return newest
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

	// Fields is the raw three-way split — software, other, unknown — rather
	// than the two views the rail draws from it. The client composes
	// "engineering" as software+unknown because that is what the feed's default
	// predicate does, and sending a pre-summed number would put the same
	// judgement in two places and let them drift.
	Fields map[string]int64 `json:"fields"`

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

// facetsSQL counts every chip in the filter rail in one pass.
//
// At package scope because it is fifty lines of SQL around fifteen lines of
// Go, and burying it made Facets read as a long function when the only long
// thing is the statement. Same reason upsertPostingSQL sits out here.
const facetsSQL = `
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
SELECT 'field', p.field, count(*)
  FROM job_postings p WHERE p.status = 'live' GROUP BY 2
UNION ALL
SELECT 'comp_undisclosed', 'n', count(*)
  FROM job_postings p WHERE p.status = 'live' AND p.comp_max IS NULL`

func Facets(ctx context.Context, pool *pgxpool.Pool) (FeedFacets, error) {
	out := FeedFacets{
		Modes:     map[string]int64{},
		Countries: map[string]int64{},
		Vendors:   map[string]int64{},
		Fields:    map[string]int64{},
		YoE:       map[string]int64{},
		Comp:      map[string]int64{},
	}

	rows, err := pool.Query(ctx, facetsSQL)
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
		case "field":
			out.Fields[value] = n
		case "comp_undisclosed":
			out.CompUndisclosed = n
		}
	}
	return out, rows.Err()
}
