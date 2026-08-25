// Package personio adapts the Personio careers XML feed.
//
//	GET https://{company}.jobs.personio.de/xml
//
// Verified unauthenticated against orderbird on 2026-08-25. The .de domain is
// not a regional variant: it is the canonical host for every tenant, including
// non-German ones, because Personio is a German company and never moved it.
//
// This is the only XML feed we consume, and the only vendor that publishes
// SENIORITY and a years-of-experience range as structured fields. Both are
// scoring inputs we otherwise have to infer from prose, which makes a Personio
// board unusually valuable per posting even though tenants are typically small.
//
// A tenant that does not exist does not 404. It 307s to personio.com, which
// sits behind a bot check — so a wrong board token yields a redirect to HTML
// rather than an error, and the parser is what has to notice.
package personio

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/jobtrack/jobtrack/internal/source"
)

const maxBodyBytes = 32 << 20

type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

func (a *Adapter) Vendor() source.Vendor { return source.VendorPersonio }

// --- wire types -------------------------------------------------------------

// feed's root element is <workzag-jobs>, Personio's pre-rebrand name. It has
// never changed and is the only reliable signal that we reached a real feed
// rather than the marketing site a bad tenant redirects to.
//
// Declaring XMLName is what enforces that: with it, xml.Unmarshal REJECTS a
// document whose root is something else, so the HTML returned for a
// non-existent tenant fails loudly here instead of unmarshalling into an empty
// struct that downstream would read as "this company closed every role". An
// explicit check after the unmarshal would be unreachable — verified, not
// assumed, by TestParse_RejectsHTMLFromABadTenant.
type feed struct {
	XMLName   xml.Name       `xml:"workzag-jobs"`
	Positions []wirePosition `xml:"position"`
}

type wirePosition struct {
	ID         string `xml:"id" json:"id"`
	Name       string `xml:"name" json:"name"`
	Subcompany string `xml:"subcompany" json:"subcompany"`
	Office     string `xml:"office" json:"office"`
	Department string `xml:"department" json:"department"`

	AdditionalOffices struct {
		Office []string `xml:"office" json:"office"`
	} `xml:"additionalOffices" json:"additionalOffices"`

	EmploymentType string `xml:"employmentType" json:"employmentType"`
	Seniority      string `xml:"seniority" json:"seniority"`
	Schedule       string `xml:"schedule" json:"schedule"`
	// A RANGE, not a number: "2-5", "1-3", or "lt-1". Passed through as prose
	// for the resume matcher rather than parsed here, because adapters do not
	// interpret — see the package doc on RawPosting.
	YearsOfExperience string `xml:"yearsOfExperience" json:"yearsOfExperience"`
	Keywords          string `xml:"keywords" json:"keywords"`
	Occupation        string `xml:"occupationCategory" json:"occupationCategory"`

	// Full ISO-8601 with an offset. Personio is one of only three vendors that
	// gives a real time of day, which is what makes its postings usable in the
	// ingest-latency measurement.
	CreatedAt string `xml:"createdAt" json:"createdAt"`

	JobDescriptions struct {
		Sections []struct {
			Name  string `xml:"name" json:"name"`
			Value string `xml:"value" json:"value"`
		} `xml:"jobDescription" json:"jobDescription"`
	} `xml:"jobDescriptions" json:"jobDescriptions"`
}

// --- fetching ---------------------------------------------------------------

func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	resp, err := source.Do(ctx, a.client, a.Vendor(), src, source.Request{
		URL:       fmt.Sprintf("https://%s.jobs.personio.de/xml", src.BoardToken),
		UserAgent: a.userAgent,
		Header:    map[string]string{"Accept": "application/xml, text/xml"},
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
	// The feed carries no links at all, so both URLs are built here from the
	// board token — which Parse does not have and should not be given, since
	// it would then be a parameter that only one vendor uses.
	for i := range postings {
		SetURLs(&postings[i], src.BoardToken)
	}
	result.Postings = postings
	return result, nil
}

func (a *Adapter) Parse(body []byte) ([]source.RawPosting, error) {
	var f feed
	if err := xml.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("personio: %w: %v", source.ErrMalformed, err)
	}

	out := make([]source.RawPosting, 0, len(f.Positions))
	for i := range f.Positions {
		p, err := a.convert(&f.Positions[i])
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (a *Adapter) convert(w *wirePosition) (source.RawPosting, error) {
	id := strings.TrimSpace(w.ID)
	if id == "" || strings.TrimSpace(w.Name) == "" {
		return source.RawPosting{}, fmt.Errorf("personio: position missing id or name: %w", source.ErrMalformed)
	}
	// JSON, not XML, even though the feed is XML: RawPosting.Raw lands in a
	// jsonb column. Marshalling the wire struct as XML produced a valid
	// document that Postgres rejected with "invalid input syntax for type
	// json", and the golden tests could not have caught it — they never touch
	// the database. Found by watching the first live poll.
	raw, _ := json.Marshal(w)

	p := source.RawPosting{
		ExternalID:      id,
		Title:           strings.TrimSpace(w.Name),
		LocationRaw:     location(w),
		DescriptionHTML: describe(w),
		Department:      strings.TrimSpace(w.Department),
		Office:          strings.TrimSpace(w.Office),
		EmploymentType:  employment(w),
		Raw:             raw,
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(w.CreatedAt)); err == nil {
		p.PostedAt = &t
	}
	return p, nil
}

// SetURLs fills the two URLs the feed does not contain.
//
// Personio's XML has no link of any kind — not to the posting, not to the
// application form — so both have to be constructed from the tenant and the id.
// That makes them the adapter's claim rather than the vendor's, which is why
// this is a separate step the caller performs with the board token it holds
// rather than something convert() guesses at.
func SetURLs(p *source.RawPosting, boardToken string) {
	p.PostingURL = fmt.Sprintf("https://%s.jobs.personio.de/job/%s", boardToken, p.ExternalID)
	p.ApplyURL = p.PostingURL + "#apply"
}

// describe joins the named sections into one HTML body.
//
// Sections arrive as {name, value} pairs — "Introduction", "Your tasks", "Your
// profile" — in the employer's own language and order. The names are kept as
// headings: dropping them runs three unrelated blocks together, and the section
// titles are often the only structure a Personio description has.
func describe(w *wirePosition) string {
	var b strings.Builder
	for _, s := range w.JobDescriptions.Sections {
		name, value := strings.TrimSpace(s.Name), strings.TrimSpace(s.Value)
		if value == "" {
			continue
		}
		if name != "" {
			b.WriteString("<h2>" + xmlEscape(name) + "</h2>\n")
		}
		b.WriteString(value)
		b.WriteString("\n")
	}
	return b.String()
}

// xmlEscape guards the heading only. Section VALUES are employer-authored HTML
// and are sanitised downstream like every other vendor's; escaping them here
// would render the markup as visible tags.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// location joins the primary office with any additional ones.
//
// A Personio position can list several offices, and normalise.Location reads the
// whole string — so dropping the extras would make a role open in Berlin and
// Munich look Berlin-only, and filter it out for half the people it suits.
func location(w *wirePosition) string {
	parts := make([]string, 0, 1+len(w.AdditionalOffices.Office))
	seen := map[string]bool{}
	for _, o := range append([]string{w.Office}, w.AdditionalOffices.Office...) {
		if o = strings.TrimSpace(o); o != "" && !seen[o] {
			seen[o] = true
			parts = append(parts, o)
		}
	}
	return strings.Join(parts, " · ")
}

// employment reads both fields, preferring whichever is more specific.
//
// They answer different questions: employmentType is the contract
// ("permanent", "working_student", "freelance"), schedule is the hours
// ("full-time", "part-time"). Our single field has to carry one of them.
//
// Schedule wins in general, because "full-time" is what the field means. But
// intern and contract are contract classes that schedule cannot express at all,
// and they are the more useful answer where they apply: orderbird's Werkstudent
// role is schedule=part-time, employmentType=working_student, and calling it
// merely part-time discards the fact that it is a student position — which is
// exactly what someone filtering for one is looking for.
func employment(w *wirePosition) string {
	byType := source.NormaliseEmployment(w.EmploymentType)
	if byType == "intern" || byType == "contract" {
		return byType
	}
	if v := source.NormaliseEmployment(w.Schedule); v != "" {
		return v
	}
	return byType
}
