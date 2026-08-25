// Package ashby adapts the Ashby public job-board API.
//
//	GET https://api.ashbyhq.com/posting-api/job-board/{board}?includeCompensation=true
//
// Ashby is the only vendor that returns STRUCTURED compensation on the list
// endpoint, which makes it disproportionately valuable: salary is one of the
// four ghost-job integrity correlates and one of the most-used filters, so a
// hundred Ashby boards are worth more to users than a hundred boards without it.
//
// Its quirk is where that compensation lives. It nests inconsistently —
// usually in `compensation.summaryComponents`, sometimes in
// `compensation.compensationTiers[].components`, occasionally only as a
// pre-rendered summary string. All three shapes have to be handled, and the
// parameter is easy to forget: omitting includeCompensation returns no salary
// at all rather than an error.
package ashby

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jobtrack/jobtrack/internal/source"
)

const (
	baseURL      = "https://api.ashbyhq.com/posting-api/job-board"
	maxBodyBytes = 16 << 20
)

type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

func (a *Adapter) Vendor() source.Vendor { return source.VendorAshby }

// --- wire types -------------------------------------------------------------

type boardResponse struct {
	Jobs []wireJob `json:"jobs"`
}

type wireJob struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Location         string `json:"location"`
	Department       string `json:"department"`
	Team             string `json:"team"`
	EmploymentType   string `json:"employmentType"`
	IsListed         bool   `json:"isListed"`
	IsRemote         bool   `json:"isRemote"`
	PublishedAt      string `json:"publishedAt"`
	JobURL           string `json:"jobUrl"`
	ApplyURL         string `json:"applyUrl"`
	DescriptionHTML  string `json:"descriptionHtml"`
	DescriptionPlain string `json:"descriptionPlain"`

	// Ashby renders a human-readable summary alongside the structured data.
	// Kept as a last-resort fallback when the structured form is absent.
	CompensationTierSummary string            `json:"compensationTierSummary"`
	Compensation            *wireCompensation `json:"compensation"`

	SecondaryLocations []struct {
		Location string `json:"location"`
	} `json:"secondaryLocations"`
}

type wireCompensation struct {
	// Shape 1: a flat summary of components.
	SummaryComponents []wireComponent `json:"summaryComponents"`
	// Shape 2: tiers, each with its own components (per-location bands).
	CompensationTiers []struct {
		Title      string          `json:"title"`
		Components []wireComponent `json:"components"`
	} `json:"compensationTiers"`
	Summary string `json:"summary"`
}

type wireComponent struct {
	// "Salary", "Equity", "Bonus" — only Salary is compensation for our purposes.
	CompensationType string `json:"compensationType"`

	// The period the figures are quoted over: "1 YEAR", "1 HOUR", "1 MONTH".
	//
	// This key used to be bound to a field called InterviewType, while the
	// field named Interval read `compensationInterval` — a key Ashby does not
	// send. Interval was therefore always empty and every rate fell through to
	// the yearly default, which put "$30 – $45 per year" in front of users for
	// a $30–45 PER HOUR contract role. Three live postings carried it.
	Interval     string   `json:"interval"`
	CurrencyCode string   `json:"currencyCode"`
	MinValue     *float64 `json:"minValue"`
	MaxValue     *float64 `json:"maxValue"`
	Summary      string   `json:"summary"`
}

// --- fetching ---------------------------------------------------------------

func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	resp, err := source.Do(ctx, a.client, a.Vendor(), src, source.Request{
		URL:       fmt.Sprintf("%s/%s?includeCompensation=true", baseURL, src.BoardToken),
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
		return nil, fmt.Errorf("ashby: %w: %v", source.ErrMalformed, err)
	}

	out := make([]source.RawPosting, 0, len(resp.Jobs))
	for i := range resp.Jobs {
		j := &resp.Jobs[i]

		// isListed=false means the employer has unpublished it. Ingesting it
		// would show users a role they cannot apply to — exactly the ghost-job
		// problem this product exists to reduce.
		if !j.IsListed {
			continue
		}
		p, err := a.convert(j)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (a *Adapter) convert(j *wireJob) (source.RawPosting, error) {
	if j.ID == "" || j.Title == "" {
		return source.RawPosting{}, fmt.Errorf("ashby: job missing id or title: %w", source.ErrMalformed)
	}

	raw, _ := json.Marshal(j)

	applyURL := j.ApplyURL
	if applyURL == "" {
		applyURL = j.JobURL
	}

	p := source.RawPosting{
		ExternalID:      j.ID,
		Title:           strings.TrimSpace(j.Title),
		LocationRaw:     buildLocation(j),
		DescriptionHTML: j.DescriptionHTML,
		ApplyURL:        applyURL,
		PostingURL:      j.JobURL,
		Department:      source.FirstNonEmpty(j.Department, j.Team),
		EmploymentType:  normaliseEmployment(j.EmploymentType),
		Raw:             raw,
	}

	// publishedAt is a genuine first-publication timestamp, so unlike
	// Greenhouse's list endpoint this is NOT an estimate.
	if j.PublishedAt != "" {
		if t, err := time.Parse(time.RFC3339, j.PublishedAt); err == nil {
			utc := t.UTC()
			p.PostedAt = &utc
		}
	}

	// isRemote is the vendor's claim, and normalisation treats it as a hint —
	// the description wins if it contradicts.
	if j.IsRemote {
		p.WorkplaceType = "remote"
	}

	applyCompensation(&p, j)
	return p, nil
}

// buildLocation joins the primary and secondary locations.
//
// Preserved as one raw string rather than picking one: the user always sees the
// original, and location parsing resolves the first concrete place from it.
func buildLocation(j *wireJob) string {
	parts := []string{strings.TrimSpace(j.Location)}
	for _, s := range j.SecondaryLocations {
		if v := strings.TrimSpace(s.Location); v != "" {
			parts = append(parts, v)
		}
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " / ")
}

// applyCompensation handles all three shapes Ashby emits.
func applyCompensation(p *source.RawPosting, j *wireJob) {
	if j.Compensation == nil {
		return
	}

	components := j.Compensation.SummaryComponents
	if len(components) == 0 {
		// Fall back to tiers, flattening across them. As with Greenhouse, the
		// widest envelope is used: a narrower range would understate the role,
		// and we cannot know which band applies to a given candidate.
		for _, tier := range j.Compensation.CompensationTiers {
			components = append(components, tier.Components...)
		}
	}

	var (
		min, max float64
		currency string
		interval string
		found    bool
	)
	for _, c := range components {
		// Equity and bonus are not salary. Including them would produce a
		// number that means nothing and compares to nothing.
		if !strings.EqualFold(c.CompensationType, "Salary") {
			continue
		}
		if c.MinValue == nil && c.MaxValue == nil {
			continue
		}
		lo, hi := valueOr(c.MinValue, 0), valueOr(c.MaxValue, 0)
		if lo == 0 && hi == 0 {
			continue
		}
		if !found {
			min, max, currency, interval, found = lo, hi, c.CurrencyCode, c.Interval, true
			continue
		}
		if c.CurrencyCode != currency {
			continue // mixing currencies produces a meaningless figure
		}
		if lo > 0 && (min == 0 || lo < min) {
			min = lo
		}
		if hi > max {
			max = hi
		}
	}

	if !found || (min <= 0 && max <= 0) {
		return
	}

	// A floor of zero means "unspecified", not "zero pounds". Recording it as a
	// number would be inventing one, so only the figures the employer actually
	// published are set — "up to $35 an hour" is a real, useful posting.
	if min > 0 {
		p.CompMin = &min
	}
	if max > 0 {
		p.CompMax = &max
	}
	p.CompCurrency = currency
	p.CompPeriod = normaliseInterval(interval)
	p.CompIsStructured = true
}

// normaliseInterval maps Ashby's period onto ours.
//
// Both spellings are accepted because both appear: the "1 HOUR" form is what
// live boards send today, and the "HOURLY" form was here first and costs
// nothing to keep.
//
// An unrecognised period returns "", not "year". Defaulting to a year is
// assuming the most common case and stating it as fact, which is how the rate
// bug above stayed invisible — every wrong answer looked like a normal salary.
// An empty period is stored as NULL, and the UI shows a range without a period
// rather than the wrong one.
func normaliseInterval(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "1 HOUR", "HOURLY", "PER_HOUR":
		return "hour"
	case "1 DAY", "DAILY", "PER_DAY":
		return "day"
	case "1 WEEK", "WEEKLY", "PER_WEEK":
		return "week"
	case "1 MONTH", "MONTHLY", "PER_MONTH":
		return "month"
	case "1 YEAR", "YEARLY", "ANNUAL", "ANNUALLY", "PER_YEAR":
		return "year"
	default:
		return ""
	}
}

func normaliseEmployment(v string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), " ", "")) {
	case "fulltime":
		return "full_time"
	case "parttime":
		return "part_time"
	case "contract", "contractor":
		return "contract"
	case "intern", "internship":
		return "intern"
	case "temporary", "temp":
		return "temporary"
	default:
		return ""
	}
}

func valueOr(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}


