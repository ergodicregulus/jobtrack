// Package recruitee adapts the Recruitee careers-site API.
//
//	GET https://{company}.recruitee.com/api/offers/
//
// Verified unauthenticated against channable, nmbrs and hotelchamp on
// 2026-08-25. Recruitee also publishes api.recruitee.com/c/{company}/offers,
// which is a different endpoint and returns 401 on every tenant tried — that one
// is the authenticated admin API and is out of scope under ADR-0004.
//
// Recruitee is the richest list endpoint of any vendor we support: it returns
// the full description, structured salary WITH a period, and explicit
// remote/hybrid/on-site flags, all without a detail fetch. Two consequences:
// there is no detail phase here at all, and salary coverage from a Recruitee
// board is close to total where the employer filled it in.
package recruitee

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jobtrack/jobtrack/internal/source"
)

const (
	// Descriptions are inline, so a board is large: 15 offers is ~420 KB.
	maxBodyBytes = 32 << 20
)

type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

func (a *Adapter) Vendor() source.Vendor { return source.VendorRecruitee }

// --- wire types -------------------------------------------------------------

type boardResponse struct {
	Offers []wireOffer `json:"offers"`
}

type wireOffer struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Slug   string `json:"slug"`
	Status string `json:"status"`

	Description  string `json:"description"`
	Requirements string `json:"requirements"`

	CareersURL      string `json:"careers_url"`
	CareersApplyURL string `json:"careers_apply_url"`

	// Both are "2026-08-19 09:02:14 UTC" — a space, not a T, and a zone
	// abbreviation rather than an offset. Not ISO-8601 and not RFC 3339.
	CreatedAt   string `json:"created_at"`
	PublishedAt string `json:"published_at"`

	City        string `json:"city"`
	StateName   string `json:"state_name"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`

	Department         string `json:"department"`
	EmploymentTypeCode string `json:"employment_type_code"`

	// Not mutually exclusive. hotelchamp returns hybrid AND on_site true on the
	// same offer, so these are read in priority order rather than as a choice.
	Remote bool `json:"remote"`
	Hybrid bool `json:"hybrid"`
	OnSite bool `json:"on_site"`

	Salary *wireSalary `json:"salary"`
}

// wireSalary quotes its figures as STRINGS, and sends an object with four nulls
// rather than omitting itself when the employer left salary blank.
type wireSalary struct {
	Min      string `json:"min"`
	Max      string `json:"max"`
	Period   string `json:"period"`
	Currency string `json:"currency"`
}

// --- fetching ---------------------------------------------------------------

func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	resp, err := source.Do(ctx, a.client, a.Vendor(), src, source.Request{
		URL:       fmt.Sprintf("https://%s.recruitee.com/api/offers/", src.BoardToken),
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
		return nil, fmt.Errorf("recruitee: %w: %v", source.ErrMalformed, err)
	}

	out := make([]source.RawPosting, 0, len(resp.Offers))
	for i := range resp.Offers {
		o := &resp.Offers[i]
		// The endpoint is documented as returning published offers only, but it
		// reports the status and a draft reaching users is the ghost-job
		// failure this product exists to reduce. Trust the field, not the doc.
		if o.Status != "" && o.Status != "published" {
			continue
		}
		p, err := a.convert(o)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (a *Adapter) convert(o *wireOffer) (source.RawPosting, error) {
	if o.ID == 0 || strings.TrimSpace(o.Title) == "" {
		return source.RawPosting{}, fmt.Errorf("recruitee: offer missing id or title: %w", source.ErrMalformed)
	}
	raw, _ := json.Marshal(o)

	p := source.RawPosting{
		ExternalID:      strconv.FormatInt(o.ID, 10),
		Title:           strings.TrimSpace(o.Title),
		LocationRaw:     location(o),
		DescriptionHTML: description(o),
		ApplyURL:        source.FirstNonEmpty(o.CareersApplyURL, o.CareersURL),
		PostingURL:      o.CareersURL,
		Department:      strings.TrimSpace(o.Department),
		Office:          strings.TrimSpace(o.City),
		EmploymentType:  source.NormaliseEmployment(o.EmploymentTypeCode),
		WorkplaceType:   workplace(o),
		Raw:             raw,
	}

	// published_at, not created_at. channable's "Open application" was created
	// in 2019 and published in 2021; created_at would make a four-year-old
	// evergreen posting look two years fresher than it is.
	if t, ok := parseTime(o.PublishedAt); ok {
		p.PostedAt = &t
	} else if t, ok := parseTime(o.CreatedAt); ok {
		p.PostedAt = &t
		p.PostedAtIsEstimate = true
	}

	applySalary(&p, o.Salary)
	return p, nil
}

// description joins the two HTML bodies Recruitee returns separately.
//
// Both are already HTML and both are sanitised downstream. The heading is added
// because the requirements block arrives with no context of its own, and a
// description that runs straight into a bullet list of years-of-experience reads
// as though the employer wrote it that way.
func description(o *wireOffer) string {
	d, r := strings.TrimSpace(o.Description), strings.TrimSpace(o.Requirements)
	switch {
	case r == "":
		return d
	case d == "":
		return r
	default:
		return d + "\n<h2>Requirements</h2>\n" + r
	}
}

func location(o *wireOffer) string {
	return strings.Join(nonEmpty(o.City, o.StateName, source.FirstNonEmpty(o.Country, o.CountryCode)), ", ")
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

// workplace reads the three flags in priority order because they are not
// mutually exclusive — hotelchamp returns hybrid and on_site together. Remote
// wins over hybrid, and hybrid over on-site, so the most permissive true claim
// is never lost. normalise.Mode still gets the last word from the prose.
func workplace(o *wireOffer) string {
	switch {
	case o.Remote:
		return "remote"
	case o.Hybrid:
		return "hybrid"
	case o.OnSite:
		return "onsite"
	default:
		return ""
	}
}

// parseTime reads Recruitee's "2026-08-19 09:02:14 UTC".
//
// The zone is an abbreviation, and Go resolves abbreviations against the local
// zone database — so parsing with a MST layout yields the right wall clock and,
// for anything but UTC, a zero offset, silently shifting the timestamp. Every
// value observed is UTC, so it is required and parsed explicitly; anything else
// is rejected rather than assumed.
func parseTime(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	rest, ok := strings.CutSuffix(v, " UTC")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", rest, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// applySalary fills the compensation fields, or leaves them nil.
//
// Recruitee sends min and max as strings and sends an object of four nulls when
// the employer left it blank, so "salary is present" is not the same question as
// "salary is disclosed". The period is passed through untouched: a 4,500–6,000
// EUR figure is monthly here and annual on other boards, and converting it in
// the adapter would bury the only evidence of which.
func applySalary(p *source.RawPosting, s *wireSalary) {
	if s == nil {
		return
	}
	period := source.NormalisePeriod(s.Period)
	min, minOK := parseAmount(s.Min)
	max, maxOK := parseAmount(s.Max)
	if !minOK && !maxOK {
		return
	}
	// A figure with no period is unusable: it could be hourly or annual, and
	// showing it either way invents the more likely one.
	if period == "" {
		return
	}
	if minOK {
		p.CompMin = &min
	}
	if maxOK {
		p.CompMax = &max
	}
	p.CompCurrency = strings.ToUpper(strings.TrimSpace(s.Currency))
	p.CompPeriod = period
	p.CompIsStructured = true
}

func parseAmount(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return f, true
}
