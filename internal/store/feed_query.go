package store

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FilterError is a query-string parameter that does not describe a valid
// filter. The api turns it into a 400; the digest skips the search it came from.
type FilterError struct {
	Field   string // the parameter
	Code    string // unknown | invalid | range
	Message string // what a valid value looks like
	Detail  string // the sentence for a human
}

func (e *FilterError) Error() string { return e.Detail }

func filterErr(field, code, message, detail string) *FilterError {
	return &FilterError{Field: field, Code: code, Message: message, Detail: detail}
}

// FeedFilterFromQuery reads a feed filter from its canonical encoding: the
// query string the feed, saved searches and shared links all use.
//
// It lives here rather than in the api because the digest has to read a saved
// search exactly as the feed reads it. It used to be the api's alone, so the
// digest did not read saved searches at all — every subscriber was emailed the
// newest roles in the whole corpus under their own search's name.
//
// An unknown parameter is an error, never ignored: a typo'd filter that quietly
// returns unfiltered results is the worst failure here, because the reader
// believes they are looking at a filtered list.
func FeedFilterFromQuery(q url.Values) (FeedFilter, error) {
	for key := range q {
		if !knownFeedParams[key] {
			return FeedFilter{}, filterErr(key, "unknown", "not a supported filter",
				"unknown query parameter: "+key)
		}
	}

	f := FeedFilter{
		Countries: csv(q.Get("country")),
		Modes:     csv(q.Get("mode")),
		Skills:    csv(q.Get("skills")),
		Vendors:   csv(q.Get("vendor")),
		Query:     strings.TrimSpace(q.Get("q")),
		Sort:      q.Get("sort"),
		// Default true. A large share of SDE-1 postings say "2+ years"; that
		// band is soft in practice and self-filtering there costs real
		// opportunities, so the stretch is opt-out rather than opt-in.
		CompDisclosedOnly: q.Get("comp_disclosed_only") == "true",
	}
	if err := parseFeedEnums(&f, q); err != nil {
		return f, err
	}
	if err := parseFeedNumbers(&f, q); err != nil {
		return f, err
	}
	return f, nil
}

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

// validBands are the scorer's bands, matching.Band*.
var validBands = map[string]bool{
	"strong": true, "plausible": true, "stretch": true, "unlikely": true,
}

var validYoEBands = map[string]bool{"0-2": true, "3-5": true, "6-8": true, "9+": true}

// parseFeedEnums checks the parameters drawn from fixed sets.
//
// Validated rather than passed through: an unknown value would return an empty
// feed, which is indistinguishable from "nothing matches you" and is the wrong
// answer to a typo.
func parseFeedEnums(f *FeedFilter, q url.Values) error {
	for _, m := range f.Modes {
		if !validModes[m] {
			return filterErr("mode", "invalid", "must be onsite, hybrid, remote or unknown",
				"invalid mode: "+m)
		}
	}
	if !validSorts[f.Sort] {
		return filterErr("sort", "invalid", "must be newest, comp, match or relevance",
			"invalid sort: "+f.Sort)
	}
	f.Field = strings.ToLower(strings.TrimSpace(q.Get("field")))
	if !validFields[f.Field] {
		return filterErr("field", "invalid", "must be software, other, unknown or all",
			"invalid field: "+f.Field)
	}
	for _, b := range csv(q.Get("band")) {
		// Lowercased, unlike the other enums: bands appear in shared URLs and
		// "Strong" is what a person types. Modes and sorts are only ever
		// produced by our own links.
		b = strings.ToLower(b)
		if !validBands[b] {
			return filterErr("band", "invalid", "must be strong, plausible, stretch or unlikely",
				"invalid band: "+b)
		}
		f.Bands = append(f.Bands, b)
	}
	return nil
}

// parseFeedNumbers reads the bounded numeric parameters.
func parseFeedNumbers(f *FeedFilter, q url.Values) error {
	// Bands, keyed as the chips and the facets are. A bare integer is accepted
	// and mapped to the band it falls in, so saved searches and links written
	// under the old threshold form keep working rather than 400ing.
	for _, raw := range csv(q.Get("yoe")) {
		band := raw
		if n, err := strconv.Atoi(raw); err == nil {
			band = yoeBandFor(n)
		}
		if !validYoEBands[band] {
			return filterErr("yoe", "invalid", "must be 0-2, 3-5, 6-8 or 9+",
				"invalid experience band: "+raw)
		}
		f.YoEBands = append(f.YoEBands, band)
	}

	if v := q.Get("comp_min"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || n < 0 {
			return filterErr("comp_min", "invalid", "non-negative number",
				"comp_min must be a non-negative number")
		}
		f.CompMin = &n
		f.CompCurrency = q.Get("comp_currency")
	}

	if v := q.Get("posted_within"); v != "" {
		d, ok := parseWindow(v)
		if !ok {
			return filterErr("posted_within", "invalid", "24h, 3d, 7d, 14d or any",
				"posted_within must be 24h, 3d, 7d, 14d or any")
		}
		f.PostedWithin = d
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			return filterErr("limit", "range", "1-50", "limit must be between 1 and 50")
		}
		f.Limit = n
	}
	return nil
}

// yoeBandFor maps a legacy threshold to its band.
func yoeBandFor(n int) string {
	switch {
	case n <= 2:
		return "0-2"
	case n <= 5:
		return "3-5"
	case n <= 8:
		return "6-8"
	default:
		return "9+"
	}
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
