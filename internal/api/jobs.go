package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/store"
)

// handleJobs serves the feed.
//
// Filtering, sorting and pagination all happen in Postgres. The client receives
// a page of ~25 already-decided results and renders them — shipping a query
// engine to the browser is what blows the interaction budget on a low-end
// laptop.
func (a *API) handleJobs(w http.ResponseWriter, r *http.Request) error {
	f, err := parseFeedFilter(r)
	if err != nil {
		return err
	}

	// The feed is public, but a signed-in viewer gets their match scores in the
	// same query. Optional rather than required auth: making discovery need an
	// account would hide the thing that demonstrates the product's value.
	if uid := httpx.UserIDFromContext(r.Context()); uid != 0 {
		f.UserID = &uid
	}

	// A band is a statement about one person's fit, so it cannot be honoured
	// without a profile to compare against. Rejecting is the only honest
	// response: silently ignoring the filter would return the unfiltered feed
	// under a heading promising strong matches, and returning nothing would
	// look like "you match nothing" rather than "we cannot know".
	if len(f.Bands) > 0 && f.UserID == nil {
		return httpx.ErrBadRequest("filtering by match band requires an account",
			httpx.FieldError{Field: "band", Code: "requires_auth",
				Message: "Sign in to filter by how well a role fits you"})
	}

	page, err := store.Feed(r.Context(), a.pool, a.scorer, f, r.URL.Query().Get("cursor"))
	if errors.Is(err, store.ErrRankingNeedsViewer) {
		return httpx.ErrBadRequest(
			"sorting by match or filtering by band needs an account — a band is a statement about your fit",
			httpx.FieldError{Field: "band", Code: "requires_auth", Message: "sign in to rank by match"})
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// The marker for "new since your last visit". Read separately from the feed
	// query because it belongs to the VIEWER, not to any posting, and folding it
	// into the row query would repeat one timestamp across every row.
	if f.UserID != nil {
		if since, err := store.PreviousVisit(r.Context(), a.pool, *f.UserID); err == nil {
			page.SinceLastVisit = since
		}
		// A failure here is not worth failing the feed over: the reader loses a
		// convenience, not the jobs.
	}

	// Personalised and freshness-critical, so never cached at the edge. A
	// cached feed is a stale feed, and freshness is the product.
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, page)
	return nil
}

// handleFacets returns counts per filter value.
func (a *API) handleFacets(w http.ResponseWriter, r *http.Request) error {
	facets, err := store.Facets(r.Context(), a.pool)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, facets)
	return nil
}

// validBands mirrors the check constraint on user_job_scores.band. A value that
// passed here but not there would surface as a 500 rather than a 400, which is
// the wrong error for bad input.
var validBands = map[string]bool{
	"strong": true, "plausible": true, "stretch": true, "unlikely": true,
}

// parseFeedFilter reads the query string.
//
// An unknown parameter is a 400, never silently ignored: a typo'd filter that
// quietly returns unfiltered results is the worst possible failure here,
// because the user believes they are looking at a filtered list.
// knownFeedParams is the allowlist. An unknown parameter is rejected rather
// than ignored: a typo that silently returns an unfiltered feed looks like the
// filter did nothing, which is the hardest kind of bug for a user to report.
var knownFeedParams = map[string]bool{
	"country": true, "mode": true, "yoe": true, "yoe_stretch": true,
	"comp_min": true, "comp_currency": true, "comp_disclosed_only": true,
	"posted_within": true, "skills": true, "vendor": true, "q": true,
	"field": true,
	"sort":  true, "limit": true, "cursor": true, "band": true,
}

var validModes = map[string]bool{
	"onsite": true, "hybrid": true, "remote": true, "unknown": true,
}

// Empty is the default — hide `other`, keep `software` and `unknown`. "all"
// turns the filter off entirely, which a reader who disagrees with the
// classifier must be able to do.
var validFields = map[string]bool{
	"": true, "software": true, "other": true, "unknown": true, "all": true,
}

var validSorts = map[string]bool{
	"": true, "newest": true, "comp": true, "match": true, "relevance": true,
}

func parseFeedFilter(r *http.Request) (store.FeedFilter, error) {
	q := r.URL.Query()
	for key := range q {
		if !knownFeedParams[key] {
			return store.FeedFilter{}, httpx.ErrBadRequest(
				"unknown query parameter: "+key,
				httpx.FieldError{Field: key, Code: "unknown", Message: "not a supported filter"})
		}
	}

	f := store.FeedFilter{
		Countries: csv(q.Get("country")),
		Modes:     csv(q.Get("mode")),
		Skills:    csv(q.Get("skills")),
		Vendors:   csv(q.Get("vendor")),
		Query:     strings.TrimSpace(q.Get("q")),
		Sort:      q.Get("sort"),
		// Default true. A large share of SDE-1 postings say "2+ years"; that
		// band is soft in practice and self-filtering there costs real
		// opportunities, so the stretch is opt-out rather than opt-in.
		YoEStretch:        q.Get("yoe_stretch") != "false",
		CompDisclosedOnly: q.Get("comp_disclosed_only") == "true",
	}

	if err := validateEnums(&f, q); err != nil {
		return f, err
	}
	if err := parseFeedNumbers(&f, q); err != nil {
		return f, err
	}
	return f, nil
}

// validateEnums checks the parameters drawn from fixed sets.
//
// Validated rather than passed through: an unknown value would return an empty
// feed, which is indistinguishable from "nothing matches you" and is the wrong
// answer to a typo.
func validateEnums(f *store.FeedFilter, q url.Values) error {
	for _, m := range f.Modes {
		if !validModes[m] {
			return httpx.ErrBadRequest("invalid mode: "+m,
				httpx.FieldError{Field: "mode", Code: "invalid",
					Message: "must be onsite, hybrid, remote or unknown"})
		}
	}
	if !validSorts[f.Sort] {
		return httpx.ErrBadRequest("invalid sort: "+f.Sort,
			httpx.FieldError{Field: "sort", Code: "invalid",
				Message: "must be newest, comp or match"})
	}
	f.Field = strings.ToLower(strings.TrimSpace(q.Get("field")))
	if !validFields[f.Field] {
		return httpx.ErrBadRequest("invalid field: "+f.Field,
			httpx.FieldError{Field: "field", Code: "invalid",
				Message: "must be software, other, unknown or all"})
	}

	for _, b := range csv(q.Get("band")) {
		// Lowercased, unlike the other enums: bands appear in shared URLs and
		// "Strong" is what a person types. Modes and sorts are only ever
		// produced by our own links.
		b = strings.ToLower(b)
		if !validBands[b] {
			return httpx.ErrBadRequest("invalid band: "+b,
				httpx.FieldError{Field: "band", Code: "invalid",
					Message: "must be strong, plausible, stretch or unlikely"})
		}
		f.Bands = append(f.Bands, b)
	}
	return nil
}

// parseFeedNumbers reads the bounded numeric parameters.
func parseFeedNumbers(f *store.FeedFilter, q url.Values) error {
	if v := q.Get("yoe"); v != "" {
		n, err := strconv.ParseInt(v, 10, 16)
		if err != nil || n < 0 || n > 50 {
			return httpx.ErrBadRequest("yoe must be between 0 and 50",
				httpx.FieldError{Field: "yoe", Code: "range", Message: "0-50"})
		}
		y := int16(n)
		f.YoE = &y
	}

	if v := q.Get("comp_min"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || n < 0 {
			return httpx.ErrBadRequest("comp_min must be a non-negative number",
				httpx.FieldError{Field: "comp_min", Code: "invalid", Message: "non-negative number"})
		}
		f.CompMin = &n
		f.CompCurrency = q.Get("comp_currency")
	}

	if v := q.Get("posted_within"); v != "" {
		d, ok := parseWindow(v)
		if !ok {
			return httpx.ErrBadRequest("posted_within must be 24h, 3d, 7d, 14d or any",
				httpx.FieldError{Field: "posted_within", Code: "invalid",
					Message: "24h, 3d, 7d, 14d or any"})
		}
		f.PostedWithin = d
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			return httpx.ErrBadRequest("limit must be between 1 and 50",
				httpx.FieldError{Field: "limit", Code: "range", Message: "1-50"})
		}
		f.Limit = n
	}
	return nil
}

// parseWindow maps the allowed freshness windows. An allowlist rather than a
// duration parser, so "posted_within=8760h" cannot quietly mean "any".
func parseWindow(v string) (time.Duration, bool) {
	switch v {
	case "24h":
		return 24 * time.Hour, true
	case "3d":
		return 72 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "14d":
		return 14 * 24 * time.Hour, true
	case "any":
		return 0, true
	}
	return 0, false
}

func csv(s string) []string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
