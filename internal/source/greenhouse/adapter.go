// Package greenhouse adapts the Greenhouse public Job Board API.
//
//	GET https://boards-api.greenhouse.io/v1/boards/{token}/jobs?content=true
//	GET https://boards-api.greenhouse.io/v1/boards/{token}/jobs/{id}?pay_transparency=true
//
// Verified against the official schema on 2026-08-15; see
// docs/research/source-catalog.md#greenhouse and the verification log.
//
// Three quirks drive almost all of the code here:
//
//  1. `content` is HTML that is ITSELF HTML-escaped. The official example shows
//     `&amp;lt;p&amp;gt;`, so it needs a decode pass before it is HTML at all.
//  2. There is no structured compensation on the list endpoint. Pay ranges live
//     inside the description text, or in `pay_input_ranges` on the per-job
//     endpoint — making Greenhouse an N+1 vendor for structured comp.
//  3. `first_published` exists only on the detail endpoint, and it is the only
//     correct field for posting age. `updated_at` moves on any edit, so using
//     it would make an edited 40-day-old posting look fresh.
package greenhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

const (
	baseURL = "https://boards-api.greenhouse.io/v1/boards"

	// The entire board arrives in one response, even for 500+ roles — there is
	// no pagination. That makes ETag and content-hash comparison unusually
	// valuable here: one conditional request covers the whole board.
	listPath = "%s/%s/jobs?content=true"

	maxBodyBytes = 32 << 20 // 32 MiB: large boards are genuinely big
)

// Adapter implements source.Adapter for Greenhouse.
type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

func (a *Adapter) Vendor() source.Vendor { return source.VendorGreenhouse }

// --- wire types -------------------------------------------------------------
//
// Named after the vendor's field names rather than ours, so a reader comparing
// this against the API docs can do so line by line.

type listResponse struct {
	Jobs []wireJob `json:"jobs"`
	Meta struct {
		Total int `json:"total"`
	} `json:"meta"`
}

type wireJob struct {
	ID            int64  `json:"id"`
	InternalJobID int64  `json:"internal_job_id"`
	Title         string `json:"title"`
	UpdatedAt     string `json:"updated_at"`
	RequisitionID string `json:"requisition_id"`
	AbsoluteURL   string `json:"absolute_url"`
	Location      struct {
		Name string `json:"name"`
	} `json:"location"`
	Content     string          `json:"content"`
	Departments []wireNamed     `json:"departments"`
	Offices     []wireOffice    `json:"offices"`
	Metadata    json.RawMessage `json:"metadata"`

	// Detail-endpoint fields. Absent from the list response, which is exactly
	// why they are pointers — "absent" and "empty" must stay distinguishable.
	FirstPublished      *string        `json:"first_published"`
	PayInputRanges      []wirePayRange `json:"pay_input_ranges"`
	IncludeAIDisclaimer *bool          `json:"include_ai_disclaimer"`
	AIDisclaimer        string         `json:"ai_disclaimer"`
	AIOptOutRequestURL  string         `json:"ai_opt_out_request_url"`
}

type wireNamed struct {
	Name string `json:"name"`
}

// wireOffice carries the office's address as well as its label: Cloudflare's
// office is named "AMER" and located "United States".
type wireOffice struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

type wirePayRange struct {
	// Cents, so integer arithmetic all the way. Never a float near money.
	MinCents     int64  `json:"min_cents"`
	MaxCents     int64  `json:"max_cents"`
	CurrencyType string `json:"currency_type"`
	Title        string `json:"title"`
	Blurb        string `json:"blurb"`
}

// --- fetching ---------------------------------------------------------------

func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	resp, err := source.Do(ctx, a.client, a.Vendor(), src, source.Request{
		URL:       fmt.Sprintf(listPath, baseURL, src.BoardToken),
		UserAgent: a.userAgent,
		MaxBody:   maxBodyBytes,
	})
	result := source.FetchResult{
		StatusCode:   resp.StatusCode,
		ETag:         resp.ETag,
		LastModified: resp.LastModified,
		ContentHash:  resp.ContentHash,
		NotModified:  resp.NotModified,
		RetryAfter:   source.ParseRetryAfter(resp.RetryAfterHeader),
	}
	if err != nil || result.NotModified {
		return result, err
	}

	postings, err := a.Parse(resp.Body)
	if err != nil {
		return result, err
	}
	result.Postings = postings
	return result, nil
}

// Parse converts a list response into postings.
//
// Takes bytes rather than a reader so golden tests exercise exactly this
// function against captured payloads, with no HTTP involved.
func (a *Adapter) Parse(body []byte) ([]source.RawPosting, error) {
	var resp listResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("greenhouse: %w: %v", source.ErrMalformed, err)
	}

	// A board reporting jobs in meta but returning none is a truncated
	// response, not a mass closure. Closing 200 postings because of a bad
	// payload is the single most damaging thing this pipeline could do.
	if len(resp.Jobs) == 0 && resp.Meta.Total > 0 {
		return nil, fmt.Errorf("greenhouse: meta.total=%d but zero jobs: %w",
			resp.Meta.Total, source.ErrSuspiciousEmpty)
	}

	out := make([]source.RawPosting, 0, len(resp.Jobs))
	for i := range resp.Jobs {
		p, err := a.convert(&resp.Jobs[i])
		if err != nil {
			// One bad row must not discard the other 499. Skip it; the
			// per-vendor parse_confidence metric surfaces a systemic problem.
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// ParseDetail converts a single-job detail response, which is where structured
// compensation, first_published and the AI disclosure live.
func (a *Adapter) ParseDetail(body []byte) (source.RawPosting, error) {
	var job wireJob
	if err := json.Unmarshal(body, &job); err != nil {
		return source.RawPosting{}, fmt.Errorf("greenhouse: %w: %v", source.ErrMalformed, err)
	}
	return a.convert(&job)
}

func (a *Adapter) convert(j *wireJob) (source.RawPosting, error) {
	if j.ID == 0 || j.Title == "" {
		return source.RawPosting{}, fmt.Errorf("greenhouse: job missing id or title: %w",
			source.ErrMalformed)
	}

	raw, _ := json.Marshal(j)

	p := source.RawPosting{
		ExternalID:      strconv.FormatInt(j.ID, 10),
		RequisitionID:   strings.TrimSpace(j.RequisitionID),
		Title:           strings.TrimSpace(j.Title),
		LocationRaw:     strings.TrimSpace(j.Location.Name),
		DescriptionHTML: decodeContent(j.Content),
		ApplyURL:        j.AbsoluteURL,
		PostingURL:      j.AbsoluteURL,
		Raw:             raw,
	}

	if len(j.Departments) > 0 {
		p.Department = j.Departments[0].Name
	}
	if len(j.Offices) > 0 {
		p.Office = source.FirstNonEmpty(j.Offices[0].Location, j.Offices[0].Name)
	}

	// first_published is correct; updated_at is a fallback that must be marked
	// as an estimate, because it moves on every edit.
	if j.FirstPublished != nil && *j.FirstPublished != "" {
		if t, err := parseTime(*j.FirstPublished); err == nil {
			p.PostedAt = &t
		}
	}
	if p.PostedAt == nil && j.UpdatedAt != "" {
		if t, err := parseTime(j.UpdatedAt); err == nil {
			p.PostedAt = &t
			p.PostedAtIsEstimate = true
		}
	}

	applyPayRanges(&p, j.PayInputRanges)

	if j.IncludeAIDisclaimer != nil {
		p.AIScreeningDisclosed = j.IncludeAIDisclaimer
		p.AIDisclaimer = strings.TrimSpace(j.AIDisclaimer)
		p.AIOptOutURL = strings.TrimSpace(j.AIOptOutRequestURL)
	}

	return p, nil
}

// applyPayRanges maps pay_input_ranges onto the posting.
//
// Multiple ranges appear when an employer publishes per-location bands. We take
// the widest envelope rather than picking one: showing a narrower range than
// the employer offers would understate the role, and we cannot know which band
// applies to a given candidate.
func applyPayRanges(p *source.RawPosting, ranges []wirePayRange) {
	var (
		min, max int64
		currency string
		found    bool
	)
	for _, r := range ranges {
		if r.MinCents <= 0 && r.MaxCents <= 0 {
			continue
		}
		if !found {
			min, max, currency, found = r.MinCents, r.MaxCents, r.CurrencyType, true
			continue
		}
		// Only merge ranges in the same currency; mixing them would produce a
		// meaningless number.
		if r.CurrencyType != currency {
			continue
		}
		if r.MinCents > 0 && r.MinCents < min {
			min = r.MinCents
		}
		if r.MaxCents > max {
			max = r.MaxCents
		}
	}
	if !found {
		return
	}

	minUnits := float64(min) / 100
	maxUnits := float64(max) / 100
	p.CompMin = &minUnits
	if max > 0 {
		p.CompMax = &maxUnits
	}
	p.CompCurrency = currency
	// Greenhouse pay transparency ranges are annual.
	p.CompPeriod = "year"
	p.CompIsStructured = true
}

// decodeContent undoes Greenhouse's HTML escaping of HTML.
//
// The API returns the description as HTML entities encoding HTML tags, so
// `<p>` arrives as `&lt;p&gt;`. Some payloads are double-escaped
// (`&amp;lt;p&amp;gt;`), which the official example itself shows.
//
// Unescaping twice is safe for correctly-escaped content because a second pass
// over real HTML is a no-op — there are no entities left to decode. It is not
// safe to unescape blindly forever, so this is bounded at two passes and the
// result is sanitised downstream regardless.
func decodeContent(s string) string {
	if s == "" {
		return ""
	}
	decoded := html.UnescapeString(s)
	if strings.Contains(decoded, "&lt;") || strings.Contains(decoded, "&gt;") {
		decoded = html.UnescapeString(decoded)
	}
	return strings.TrimSpace(decoded)
}

// parseTime handles the formats Greenhouse actually emits.
//
// The docs show ISO-8601 with an offset, but boards in the wild also return
// plain UTC and date-only values. Accepting all three is cheaper than an
// alerting rule about a date that failed to parse.
func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z0700",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("greenhouse: unrecognised time %q", s)
}
