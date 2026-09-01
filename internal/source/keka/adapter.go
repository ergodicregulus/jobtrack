// Package keka adapts the Keka careers-site API.
//
//	GET https://{tenant}.keka.com/careers/api/embedjobs/{portal}/active/{orgID}
//
// Verified unauthenticated against spyneai, awfis and softprodigy on
// 2026-09-01: 28 live postings, full descriptions, structured locations with
// ISO country codes, and a real publication timestamp.
//
// KEKA IS INDIA-FIRST, and that is why it was built ahead of the other verified
// candidate. India was 7.1% of a corpus for a product that names India as its
// primary market; every adapter before this one was US or EU first.
//
// THE BOARD TOKEN IS TWO PARTS — "{portal}/{orgID}" — because the org
// identifier is a per-tenant GUID that cannot be derived from the company name.
// It is read once from the careers page and stored, the same shape the Workday
// adapter uses for its dc/tenant/site triple. A guessed GUID does not 404, it
// returns an empty array, which is indistinguishable from a company that has
// stopped hiring — so tokens here are verified, never guessed.
package keka

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
	// Descriptions are inline and long — 28 postings is roughly 400 KB.
	maxBodyBytes  = 32 << 20
	defaultPortal = "default"
)

type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

func (a *Adapter) Vendor() source.Vendor { return source.VendorKeka }

// --- wire types -------------------------------------------------------------

// The response is a bare JSON array, not an object with a jobs key.
type wireJob struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Excerpt     string `json:"excerpt"`
	JobNumber   string `json:"jobNumber"`

	DepartmentName string `json:"departmentName"`

	// Full ISO-8601 with a time of day, so Keka postings can feed the
	// ingest-latency measurement — unlike Workable, whose date has no clock.
	PublishedOn string `json:"publishedOn"`

	// A range as a STRING: "1", "2-5". Prose for the matcher, not a number.
	Experience string `json:"experience"`

	SkillNames   []string       `json:"skillNames"`
	JobLocations []wireLocation `json:"jobLocations"`

	// JobType and SalaryRange.SalaryPeriod are integer enums whose meaning we
	// have not established. See the note on convert.
	JobType     int         `json:"jobType"`
	SalaryRange *wireSalary `json:"salaryRange"`
}

type wireLocation struct {
	Name        string `json:"name"`
	City        string `json:"city"`
	State       string `json:"state"`
	CountryCode string `json:"countryCode"`
	CountryName string `json:"countryName"`
}

type wireSalary struct {
	Minimum      *float64 `json:"minimum"`
	Maximum      *float64 `json:"maximum"`
	Currency     string   `json:"currency"`
	SalaryPeriod int      `json:"salaryPeriod"`
}

// --- fetching ---------------------------------------------------------------

// splitToken reads "{portal}/{orgID}", or a bare orgID with the default portal.
func splitToken(token string) (portal, orgID string, err error) {
	parts := strings.Split(strings.Trim(token, "/"), "/")
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			return "", "", fmt.Errorf("keka: empty board token")
		}
		return defaultPortal, parts[0], nil
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("keka: malformed board token %q", token)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("keka: board token %q is not portal/orgID", token)
	}
}

// Tenant is the subdomain, kept beside the token because the URL needs both.
// The adapter reads it from the source's board token prefix when present.
func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	tenant, rest, ok := strings.Cut(src.BoardToken, ":")
	if !ok {
		return source.FetchResult{}, fmt.Errorf(
			"keka: board token %q is not tenant:portal/orgID", src.BoardToken)
	}
	portal, orgID, err := splitToken(rest)
	if err != nil {
		return source.FetchResult{}, err
	}

	resp, err := source.Do(ctx, a.client, a.Vendor(), src, source.Request{
		URL: fmt.Sprintf("https://%s.keka.com/careers/api/embedjobs/%s/active/%s",
			tenant, portal, orgID),
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
	for i := range postings {
		setURLs(&postings[i], tenant)
	}
	result.Postings = postings
	return result, nil
}

func (a *Adapter) Parse(body []byte) ([]source.RawPosting, error) {
	var jobs []wireJob
	if err := json.Unmarshal(body, &jobs); err != nil {
		return nil, fmt.Errorf("keka: %w: %v", source.ErrMalformed, err)
	}

	out := make([]source.RawPosting, 0, len(jobs))
	for i := range jobs {
		p, err := a.convert(&jobs[i])
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// convert maps one job.
//
// TWO FIELDS ARE DELIBERATELY NOT MAPPED, and both are integer enums whose
// meaning Keka does not document publicly and which the data does not reveal:
//
//   - jobType was 2 for all 16 postings on the board we sampled, spanning an
//     intern, an associate product manager, a business analyst and a data
//     engineer. Whatever it distinguishes, it is not employment type on this
//     evidence, so EmploymentType is left empty rather than guessed.
//
//   - salaryRange.salaryPeriod took the values 0 and 4 over figures of the SAME
//     magnitude — 1,800,000 INR at period 0 and 900,000 INR at period 4 — so it
//     cannot be read as year/month from the numbers. Structured compensation is
//     therefore NOT emitted: publishing a monthly figure as annual is precisely
//     the Ashby interval bug that put "$30 - $45 per year" in front of users for
//     an hourly contract. The description still goes through text extraction,
//     which has the plausibility guard.
//
// Both are recorded rather than silently dropped, so the next person can decode
// them from a wider sample instead of rediscovering the ambiguity.
func (a *Adapter) convert(j *wireJob) (source.RawPosting, error) {
	if j.ID == 0 || strings.TrimSpace(j.Title) == "" {
		return source.RawPosting{}, fmt.Errorf("keka: job missing id or title: %w", source.ErrMalformed)
	}
	raw, _ := json.Marshal(j)

	p := source.RawPosting{
		ExternalID:      strconv.FormatInt(j.ID, 10),
		RequisitionID:   strings.TrimSpace(j.JobNumber),
		Title:           strings.TrimSpace(j.Title),
		LocationRaw:     location(j),
		DescriptionHTML: description(j),
		Department:      strings.TrimSpace(j.DepartmentName),
		Office:          firstCity(j),
		Raw:             raw,
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(j.PublishedOn)); err == nil {
		p.PostedAt = &t
	}
	return p, nil
}

// setURLs builds the public address. The feed carries no link of its own, so
// like Personio these are the adapter's claim rather than the vendor's.
func setURLs(p *source.RawPosting, tenant string) {
	p.PostingURL = fmt.Sprintf("https://%s.keka.com/careers/jobdetails/%s", tenant, p.ExternalID)
	p.ApplyURL = p.PostingURL
}

// description prefers the full body and falls back to the excerpt.
//
// skillNames is appended when present. It is the employer's own list and is
// often empty, but where it exists it is a far better signal than anything
// extraction can recover from prose — and the vocabulary reads it back out of
// the text, so it reaches posting_skills through the existing path rather than
// a second one.
func description(j *wireJob) string {
	body := strings.TrimSpace(j.Description)
	if body == "" {
		body = strings.TrimSpace(j.Excerpt)
	}
	if len(j.SkillNames) == 0 {
		return body
	}
	return body + "\n<h2>Skills</h2>\n<p>" + strings.Join(j.SkillNames, ", ") + "</p>"
}

// location joins every listed office. A role open in two cities must not look
// open in one — normalise reads the whole string.
func location(j *wireJob) string {
	parts := make([]string, 0, len(j.JobLocations))
	seen := map[string]bool{}
	for _, l := range j.JobLocations {
		one := source.FirstNonEmpty(l.Name,
			strings.Join(nonEmpty(l.City, l.State, source.FirstNonEmpty(l.CountryName, l.CountryCode)), ", "))
		if one != "" && !seen[one] {
			seen[one] = true
			parts = append(parts, one)
		}
	}
	return strings.Join(parts, " · ")
}

func firstCity(j *wireJob) string {
	for _, l := range j.JobLocations {
		if c := strings.TrimSpace(l.City); c != "" {
			return c
		}
	}
	return ""
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
