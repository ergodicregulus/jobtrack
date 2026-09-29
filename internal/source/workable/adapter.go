// Package workable adapts the Workable careers-widget API.
//
//	GET https://apply.workable.com/api/v1/widget/accounts/{account}?details=true
//
// Verified unauthenticated against blueground, skroutz and epignosis on
// 2026-08-25. `details=true` is the whole reason this is a one-request adapter:
// without it the response carries no description at all, and the obvious
// alternative — a detail fetch per posting — would multiply a nine-posting
// board into ten requests for data the list can return in one.
//
// Two facts about this endpoint that shape everything below:
//
//   - An account that exists but has no live vacancies returns 200 with an
//     empty jobs array, identical to a board that just closed every role. That
//     ambiguity is why the empty case is handled explicitly rather than left to
//     look like a mass closure.
//   - published_on is a DATE, with no time of day. Every consequence of that is
//     documented on postedAt below.
package workable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

const maxBodyBytes = 32 << 20

type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

func (a *Adapter) Vendor() source.Vendor { return source.VendorWorkable }

// --- wire types -------------------------------------------------------------

type boardResponse struct {
	Name string    `json:"name"`
	Jobs []wireJob `json:"jobs"`
}

type wireJob struct {
	Title string `json:"title"`
	// Shortcode is the stable id and the one that appears in every URL.
	// `code` is the employer's own requisition reference and is often blank.
	Shortcode string `json:"shortcode"`
	Code      string `json:"code"`

	Department     string `json:"department"`
	EmploymentType string `json:"employment_type"`
	Telecommuting  bool   `json:"telecommuting"`

	URL            string `json:"url"`
	Shortlink      string `json:"shortlink"`
	ApplicationURL string `json:"application_url"`

	// "2026-06-16" — a date, never a time. See postedAt.
	PublishedOn string `json:"published_on"`
	CreatedAt   string `json:"created_at"`

	Country string `json:"country"`
	City    string `json:"city"`
	State   string `json:"state"`

	// Present only with details=true.
	Description string `json:"description"`

	Locations []struct {
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		City        string `json:"city"`
		Region      string `json:"region"`
	} `json:"locations"`
}

// --- fetching ---------------------------------------------------------------

func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	resp, err := source.Do(ctx, a.client, a.Vendor(), src, source.Request{
		URL: fmt.Sprintf(
			"https://apply.workable.com/api/v1/widget/accounts/%s?details=true", src.BoardToken),
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

func (a *Adapter) Parse(body []byte) ([]source.RawPosting, error) {
	var resp boardResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("workable: %w: %v", source.ErrMalformed, err)
	}

	out := make([]source.RawPosting, 0, len(resp.Jobs))
	for i := range resp.Jobs {
		p, err := a.convert(&resp.Jobs[i])
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (a *Adapter) convert(j *wireJob) (source.RawPosting, error) {
	if j.Shortcode == "" || strings.TrimSpace(j.Title) == "" {
		return source.RawPosting{}, fmt.Errorf("workable: job missing shortcode or title: %w", source.ErrMalformed)
	}
	raw, _ := json.Marshal(j)

	p := source.RawPosting{
		ExternalID:      j.Shortcode,
		RequisitionID:   strings.TrimSpace(j.Code),
		Title:           strings.TrimSpace(j.Title),
		LocationRaw:     location(j),
		DescriptionHTML: j.Description,
		ApplyURL:        source.FirstNonEmpty(j.ApplicationURL, j.URL, j.Shortlink),
		PostingURL:      source.FirstNonEmpty(j.URL, j.Shortlink),
		Department:      strings.TrimSpace(j.Department),
		Office:          strings.TrimSpace(j.City),
		EmploymentType:  source.NormaliseEmployment(j.EmploymentType),
		Raw:             raw,
	}
	if j.Telecommuting {
		p.WorkplaceType = "remote"
	}
	if t, ok := postedAt(j); ok {
		p.PostedAt = &t
		// Flagged as an estimate because it is date-granular, not because the
		// date is wrong. The UI presents age as an upper bound, which is the
		// honest reading of "some time on 16 June".
		p.PostedAtIsEstimate = true
	}
	return p, nil
}

// postedAt reads published_on, which has no time of day.
//
// Parsed as midnight UTC, which makes a posting look up to 24 hours OLDER than
// it is. That direction is deliberate and it is the only safe one: the opposite
// rounding would make a day-old posting look brand new, and posting age is the
// signal the whole product is built on. An adapter that rounds age downward is
// an adapter that manufactures freshness.
//
// It also means Workable postings cannot contribute to the ingest-latency
// measurement, which needs a real publication time — scripts/ingest-latency.sql
// excludes date-only sources by construction rather than by a vendor list.
func postedAt(j *wireJob) (time.Time, bool) {
	for _, v := range []string{j.PublishedOn, j.CreatedAt} {
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(v)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// location prefers the structured locations array over the flat fields.
//
// A Workable posting can be open in several cities, and the flat country/city
// pair only ever holds the first. Using it alone makes a role open in Athens and
// Berlin look Athens-only, and filters it away from half the people it suits.
func location(j *wireJob) string {
	if len(j.Locations) == 0 {
		return strings.Join(nonEmpty(j.City, j.State, j.Country), ", ")
	}
	seen := map[string]bool{}
	parts := make([]string, 0, len(j.Locations))
	for _, l := range j.Locations {
		one := strings.Join(nonEmpty(l.City, l.Region, source.FirstNonEmpty(l.Country, l.CountryCode)), ", ")
		if one != "" && !seen[one] {
			seen[one] = true
			parts = append(parts, one)
		}
	}
	return strings.Join(parts, " · ")
}

func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
