// Package smartrecruiters adapts the SmartRecruiters public postings API.
//
// The third ATS vendor, and the first that required a two-phase fetch: the list
// endpoint carries structured metadata but no description, so the body of each
// posting is a second request. That is a real cost and is bounded rather than
// hidden — see Fetch.
//
// It earns its place on capability rather than breadth. SmartRecruiters splits
// a posting into named sections, and one of them is **qualifications**. Every
// other source hands us one undifferentiated blob, which is why 73.7% of
// extracted skills land as merely `mentioned` — the must/nice split needs an
// explicit requirements heading and most postings have none. Here the heading
// is structural, so these postings produce real must-haves instead of
// abstaining, which is worth more than the postings themselves.
//
// It also closes the thinnest coverage gap in the corpus: Bosch alone publishes
// ~527 live roles in India against 278 from every other source combined.
package smartrecruiters

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jobtrack/jobtrack/internal/source"
)

const (
	baseURL = "https://api.smartrecruiters.com/v1"

	// PublicBase is the careers site, not the API. A posting's public address is
	// this plus the board token and the posting id.
	//
	// Exported because the data migration that backfills stored postings builds
	// the same URL in SQL, and two spellings of one address is a thing that can
	// drift silently.
	PublicBase = "https://jobs.smartrecruiters.com"

	// pageSize is the vendor's maximum. Fewer pages means fewer round trips and
	// a shorter window in which the list can shift under us.
	pageSize = 100

	// maxPages bounds one poll, so a pagination bug cannot walk forever against
	// someone else's API.
	//
	// It was 20 — 2,000 postings — described as "comfortably more than any board
	// we watch". BoschGroup returned 4,770 on 2026-08-19, so the cap was
	// silently discarding more than half the board, and ReconcileAbsent read the
	// missing half as postings that had closed. Sized against the measurement
	// now, with headroom; TestMaxPages_CoversTheLargestBoardWeWatch fails if a
	// board outgrows it.
	maxPages = 60

	// maxDetailFetches bounds the second phase per poll, so a first poll of a
	// large board does not look like an attack.
	//
	// The adapter cannot tell which bodies the store already holds — Fetch takes
	// a Source, not the corpus — so the window walks the board in order and
	// Source.DetailCursor remembers where it stopped. An earlier comment here
	// claimed ingest was incremental and that the window therefore only covered
	// new postings; nothing implemented that, and the window sat at [0, 250) on
	// every poll.
	maxDetailFetches = 250

	// detailConcurrency is deliberately modest. This is someone else's API and
	// we are an uninvited guest on it; four in flight fills the pipe without
	// being rude.
	detailConcurrency = 4
)

type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

func (a *Adapter) Vendor() source.Vendor { return source.VendorSmartRecruiters }

type listResponse struct {
	Offset     int       `json:"offset"`
	Limit      int       `json:"limit"`
	TotalFound int       `json:"totalFound"`
	Content    []wireJob `json:"content"`
}

type wireJob struct {
	ID string `json:"id"`

	// The board token, as the vendor spells it. Carried so Parse and ParseDetail
	// can derive a public URL without being handed the Source they were
	// deliberately built not to need.
	Company struct {
		Identifier string `json:"identifier"`
	} `json:"company"`

	UUID         string    `json:"uuid"`
	Name         string    `json:"name"`
	RefNumber    string    `json:"refNumber"`
	ReleasedDate string    `json:"releasedDate"`
	PostingURL   string    `json:"postingUrl"`
	ApplyURL     string    `json:"applyUrl"`
	Location     wireLoc   `json:"location"`
	Department   wireNamed `json:"department"`
	Function     wireNamed `json:"function"`
	TypeOfEmp    wireNamed `json:"typeOfEmployment"`
	ExpLevel     wireNamed `json:"experienceLevel"`
	JobAd        *wireAd   `json:"jobAd"`
}

type wireNamed struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type wireLoc struct {
	City         string `json:"city"`
	Region       string `json:"region"`
	Country      string `json:"country"`
	Remote       bool   `json:"remote"`
	Hybrid       bool   `json:"hybrid"`
	FullLocation string `json:"fullLocation"`
}

type wireAd struct {
	Sections map[string]wireSection `json:"sections"`
}

type wireSection struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Fetch enumerates the board, then fills in the bodies.
//
// Conditional requests are sent and honoured, but SmartRecruiters does not
// return an ETag on this endpoint in practice, so the content hash below is
// what actually saves work: an unchanged board hashes identically and the whole
// detail phase is skipped.
func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	var (
		all    []wireJob
		result source.FetchResult
		hash   = sha256.New()
	)

	for page := 0; page < maxPages; page++ {
		url := fmt.Sprintf("%s/companies/%s/postings?limit=%d&offset=%d",
			baseURL, src.BoardToken, pageSize, page*pageSize)

		body, res, err := a.get(ctx, url, src, page == 0)
		if err != nil {
			return source.FetchResult{}, err
		}
		if page == 0 {
			result.StatusCode = res.StatusCode
			result.ETag = res.Header.Get("ETag")
			result.LastModified = res.Header.Get("Last-Modified")
			result.RetryAfter = source.ParseRetryAfter(res.Header.Get("Retry-After"))

			if res.StatusCode == http.StatusNotModified {
				result.NotModified = true
				if result.ETag == "" {
					result.ETag = src.ETag
				}
				if result.LastModified == "" {
					result.LastModified = src.LastModified
				}
				return result, nil
			}
			if res.StatusCode != http.StatusOK {
				return result, fmt.Errorf("smartrecruiters: %s returned %d",
					src.BoardToken, res.StatusCode)
			}
		}

		hash.Write(body)

		var lr listResponse
		if err := json.Unmarshal(body, &lr); err != nil {
			return result, fmt.Errorf("smartrecruiters: decode page %d: %w", page, err)
		}
		all = append(all, lr.Content...)

		// Stop on a short page rather than trusting totalFound, which shifts
		// while we walk.
		if len(lr.Content) < pageSize {
			break
		}
	}

	result.ContentHash = hash.Sum(nil)

	// Resume the sweep where the last poll stopped. A board larger than the
	// budget takes several polls to come round once, and until it has, an
	// identical board is NOT a reason to skip the detail phase — the postings
	// past the cursor still have no body.
	start := src.DetailCursor
	if start >= len(all) {
		// The board has been swept. Now an identical board means identical
		// postings, and skipping is the difference between one request per poll
		// and hundreds.
		if len(src.ContentHash) > 0 && string(result.ContentHash) == string(src.ContentHash) {
			result.NotModified = true
			result.DetailCursor = start
			return result, nil
		}
		// The board moved. Sweep again from the top: positions shift as postings
		// open and close, so there is no position that is reliably "the new
		// ones", and a second pass also refreshes bodies that have been edited.
		start = 0
	}

	end := start + maxDetailFetches
	if end > len(all) {
		end = len(all)
	}
	filled := a.fillDescriptions(ctx, src, all[start:end])

	// Only step over a window we actually read.
	//
	// A body we could not fetch is fine — it leaves a posting scoring honestly
	// as an abstention, and the next full pass retries it. A window where we
	// fetched NOTHING is a different event: it means the run was cut off (a
	// cancelled context, an exhausted pool), and stepping over it would skip
	// those postings until the sweep came round again. That happened on the
	// first live run — two boards advanced 250 places having filled nothing,
	// while the database pool was saturated.
	if filled == 0 && ctx.Err() != nil {
		result.DetailCursor = start
	} else {
		result.DetailCursor = end
	}

	postings := make([]source.RawPosting, 0, len(all))
	for i := range all {
		p, err := a.convert(&all[i], src.BoardToken)
		if err != nil {
			// One malformed posting must not lose the other 4,801. The board is
			// someone else's data and will contain surprises.
			continue
		}
		postings = append(postings, p)
	}
	result.Postings = postings
	return result, nil
}

// fillDescriptions performs the second phase over one window of the board,
// bounded and concurrent. The caller chooses the window; this fills all of it.
//
// Failures are deliberately silent per posting: a body we could not fetch
// leaves a posting with metadata and no description, which scores honestly as
// an abstention. Dropping the posting entirely would be worse — the role is
// real and the user can still read it on the employer's site.
// It returns how many bodies it actually filled, which is what tells the caller
// whether the sweep made progress.
func (a *Adapter) fillDescriptions(ctx context.Context, src source.Source, jobs []wireJob) int {
	type work struct{ i int }

	todo := make(chan work)
	done := make(chan struct{})

	var (
		mu     sync.Mutex
		filled int
	)

	for w := 0; w < detailConcurrency; w++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for item := range todo {
				j := &jobs[item.i]
				url := fmt.Sprintf("%s/companies/%s/postings/%s", baseURL, src.BoardToken, j.ID)
				body, res, err := a.get(ctx, url, source.Source{}, false)
				if err != nil || res.StatusCode != http.StatusOK {
					continue
				}
				var detail wireJob
				if err := json.Unmarshal(body, &detail); err != nil {
					continue
				}
				j.JobAd = detail.JobAd
				mu.Lock()
				filled++
				mu.Unlock()
				if detail.ApplyURL != "" {
					j.ApplyURL = detail.ApplyURL
				}
				if detail.PostingURL != "" {
					j.PostingURL = detail.PostingURL
				}
			}
		}()
	}

handout:
	for i := range jobs {
		select {
		case todo <- work{i}:
		case <-ctx.Done():
			// Stop handing out work; the workers drain what they hold and the
			// close below unblocks them.
			break handout
		}
	}
	close(todo)
	for w := 0; w < detailConcurrency; w++ {
		<-done
	}
	return filled
}

// publicURL is the vendor's canonical public address for a posting.
//
// The title slug the detail document appends is decorative; the id alone
// resolves. Returns empty for an empty board token so a misconfigured source
// produces no link rather than a broken one.
func publicURL(boardToken, id string) string {
	if boardToken == "" || id == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s", PublicBase, boardToken, id)
}

func (a *Adapter) get(ctx context.Context, url string, src source.Source, conditional bool) ([]byte, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("smartrecruiters: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.userAgent)
	if conditional {
		if src.ETag != "" {
			req.Header.Set("If-None-Match", src.ETag)
		}
		if src.LastModified != "" {
			req.Header.Set("If-Modified-Since", src.LastModified)
		}
	}

	res, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("smartrecruiters: fetch: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()

	if res.StatusCode == http.StatusNotModified {
		return nil, res, nil
	}

	// Bounded: this is an external body and an unbounded read is an
	// out-of-memory kill waiting for a bad day.
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, res, fmt.Errorf("smartrecruiters: read body: %w", err)
	}
	return body, res, nil
}

// Parse converts a LIST response. Separate from Fetch so the mapping is
// testable against a captured page with no network involved.
func (a *Adapter) Parse(body []byte) ([]source.RawPosting, error) {
	var lr listResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		return nil, fmt.Errorf("smartrecruiters: decode list: %w", err)
	}
	out := make([]source.RawPosting, 0, len(lr.Content))
	for i := range lr.Content {
		p, err := a.convert(&lr.Content[i], "")
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// ParseDetail converts a single posting response, which is the only place the
// description sections exist.
func (a *Adapter) ParseDetail(body []byte) (source.RawPosting, error) {
	var j wireJob
	if err := json.Unmarshal(body, &j); err != nil {
		return source.RawPosting{}, fmt.Errorf("smartrecruiters: decode detail: %w", err)
	}
	return a.convert(&j, "")
}

var errNoID = errors.New("smartrecruiters: posting has no id")

func (a *Adapter) convert(j *wireJob, boardToken string) (source.RawPosting, error) {
	if j.ID == "" {
		return source.RawPosting{}, errNoID
	}

	raw, _ := json.Marshal(j)

	// The list response carries no applyUrl and no postingUrl — only the detail
	// document does. A posting whose body has not been fetched yet would
	// therefore reach the feed with nothing to click, which is the one thing
	// this product exists to hand over: 5,050 live postings, a third of the
	// corpus, were in exactly that state.
	//
	// The public URL is derivable. SmartRecruiters serves
	// jobs.smartrecruiters.com/{company}/{postingId} and the title slug the
	// detail document appends is optional — verified 200 against a live posting
	// on 2026-08-24. Deriving it is not guessing at a number; it is the vendor's
	// own canonical address for the posting we are holding the id of.
	fallbackURL := publicURL(source.FirstNonEmpty(boardToken, j.Company.Identifier), j.ID)

	p := source.RawPosting{
		ExternalID: j.ID,
		// refNumber is the employer's own requisition code and is a free dedup
		// key: two postings sharing it are the same role, with no similarity
		// computation.
		RequisitionID:   strings.TrimSpace(j.RefNumber),
		Title:           strings.TrimSpace(j.Name),
		LocationRaw:     locationOf(j.Location),
		DescriptionHTML: descriptionOf(j.JobAd),
		ApplyURL:        source.FirstNonEmpty(j.ApplyURL, j.PostingURL, fallbackURL),
		PostingURL:      source.FirstNonEmpty(j.PostingURL, j.ApplyURL, fallbackURL),
		Department:      j.Department.Label,
		EmploymentType:  j.TypeOfEmp.Label,
		WorkplaceType:   workplaceOf(j.Location),
		Raw:             raw,
	}

	// releasedDate is the vendor's FIRST-published timestamp, not an updated-at,
	// so freshness is real here rather than an estimate. That is unusually good
	// — most vendors only expose the latter, and an edited 40-day-old posting
	// then looks new.
	if t, err := time.Parse(time.RFC3339, j.ReleasedDate); err == nil {
		p.PostedAt = &t
	}

	return p, nil
}

// descriptionOf assembles the sections into one document, preserving the
// section HEADINGS.
//
// The headings are the entire reason this vendor is worth adapting. The skill
// extractor classifies a skill by the heading above it, and "Qualifications" is
// one of the headings it recognises as a requirements section — so these
// postings yield real must-haves where a single undifferentiated blob yields
// only `mentioned`.
//
// companyDescription is deliberately LAST. It is boilerplate about the employer
// and it is where phrases like "we use Go and Kubernetes across the group"
// live; putting it above the qualifications would let company-wide technology
// name-drops be read as requirements for this specific role.
func descriptionOf(ad *wireAd) string {
	if ad == nil || len(ad.Sections) == 0 {
		return ""
	}
	order := []string{"jobDescription", "qualifications", "additionalInformation", "companyDescription"}

	var b strings.Builder
	for _, key := range order {
		sec, ok := ad.Sections[key]
		if !ok || strings.TrimSpace(sec.Text) == "" {
			continue
		}
		title := sec.Title
		if title == "" {
			title = key
		}
		// An h2 rather than the vendor's own markup: the extractor reads
		// headings line by line after HTML stripping, and a consistent shape
		// here means one less vendor quirk downstream.
		b.WriteString("<h2>")
		b.WriteString(title)
		b.WriteString("</h2>\n")
		b.WriteString(sec.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func locationOf(l wireLoc) string {
	if s := strings.TrimSpace(l.FullLocation); s != "" {
		return s
	}
	parts := make([]string, 0, 3)
	for _, s := range []string{l.City, l.Region, l.Country} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// workplaceOf reports the vendor's CLAIM, never a conclusion.
//
// Downstream treats this as a hint and lets the description override it: a
// posting flagged remote whose body says "3 days in the office" is hybrid.
func workplaceOf(l wireLoc) string {
	switch {
	case l.Remote:
		return "remote"
	case l.Hybrid:
		return "hybrid"
	default:
		return ""
	}
}


