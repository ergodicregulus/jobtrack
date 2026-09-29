// Package bamboohr adapts the BambooHR careers API.
//
//	GET https://{company}.bamboohr.com/careers/list
//	GET https://{company}.bamboohr.com/careers/{id}/detail
//
// Verified unauthenticated on flyio, posthog and palantir, 2026-09-01.
//
// TWO PHASES, BECAUSE THE LIST HAS NO BODY. The list carries a title, a
// department and a location and nothing else; the description, the posting date
// and the seniority all live on the detail document. An adapter that shipped the
// list alone would add postings that can never be scored on skills — which is
// the exact hole the corpus spent this week climbing out of.
//
// A browser User-Agent is required. Both endpoints answer 403 to a bare client,
// which is bot-shaping rather than authentication: no key, no session, no
// CAPTCHA, and the same public data a careers page shows. Sending a normal UA is
// how a normal client identifies itself, not a bypass — contrast Darwinbox,
// which sits behind a Cloudflare challenge and is out of scope under ADR-0004.
package bamboohr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

const (
	maxBodyBytes = 16 << 20

	// detailsPerPoll bounds the second phase. BambooHR boards are small — ten
	// postings is typical — so this covers a whole board in one poll and the
	// sweep-resume machinery never has to engage.
	detailsPerPoll = 60

	// detailConcurrency is deliberately low. These are small boards and there is
	// nothing to gain from hammering them.
	detailConcurrency = 4

	// browserUA is required; see the package comment.
	browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

func (a *Adapter) Vendor() source.Vendor { return source.VendorBambooHR }

// --- wire types -------------------------------------------------------------

type listResponse struct {
	Meta struct {
		TotalCount int `json:"totalCount"`
	} `json:"meta"`
	Result []wireJob `json:"result"`
}

type wireJob struct {
	ID              string   `json:"id"`
	Name            string   `json:"jobOpeningName"`
	DepartmentLabel string   `json:"departmentLabel"`
	EmploymentType  string   `json:"employmentType"`
	IsRemote        *bool    `json:"isRemote"`
	Location        *wireLoc `json:"location"`
	ATSLocation     *wireLoc `json:"atsLocation"`

	// Filled from the detail document.
	ShareURL          string `json:"jobOpeningShareUrl"`
	Description       string `json:"description"`
	DatePosted        string `json:"datePosted"`
	MinimumExperience string `json:"minimumExperience"`
}

type wireLoc struct {
	City           string `json:"city"`
	State          string `json:"state"`
	Country        string `json:"country"`
	AddressCountry string `json:"addressCountry"`
}

type detailResponse struct {
	Result struct {
		JobOpening wireJob `json:"jobOpening"`
	} `json:"result"`
}

// --- fetching ---------------------------------------------------------------

func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	resp, err := source.Do(ctx, a.client, a.Vendor(), src, source.Request{
		URL:       fmt.Sprintf("https://%s.bamboohr.com/careers/list", src.BoardToken),
		UserAgent: browserUA,
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

	var lr listResponse
	if err := json.Unmarshal(resp.Body, &lr); err != nil {
		return result, fmt.Errorf("bamboohr: %w: %v", source.ErrMalformed, err)
	}

	jobs := lr.Result
	if len(jobs) > detailsPerPoll {
		jobs = jobs[:detailsPerPoll]
	}
	a.fillDetails(ctx, src, jobs)

	postings := make([]source.RawPosting, 0, len(jobs))
	for i := range jobs {
		p, err := a.convert(&jobs[i], src.BoardToken)
		if err != nil {
			continue
		}
		postings = append(postings, p)
	}
	result.Postings = postings
	return result, nil
}

// Parse reads a list response WITHOUT the detail phase.
//
// Present because the Adapter interface requires it and golden tests exercise
// it, but a posting from here has no body: the list simply does not carry one.
// Fetch is what produces a complete posting.
func (a *Adapter) Parse(body []byte) ([]source.RawPosting, error) {
	var lr listResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		return nil, fmt.Errorf("bamboohr: %w: %v", source.ErrMalformed, err)
	}
	out := make([]source.RawPosting, 0, len(lr.Result))
	for i := range lr.Result {
		p, err := a.convert(&lr.Result[i], "")
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// ParseDetail merges a detail document onto a list entry.
func (a *Adapter) ParseDetail(body []byte, into *wireJob) error {
	var dr detailResponse
	if err := json.Unmarshal(body, &dr); err != nil {
		return fmt.Errorf("bamboohr: decode detail: %w", err)
	}
	d := dr.Result.JobOpening
	into.Description = d.Description
	into.DatePosted = d.DatePosted
	into.MinimumExperience = d.MinimumExperience
	into.ShareURL = d.ShareURL
	if d.Location != nil {
		into.Location = d.Location
	}
	if d.ATSLocation != nil {
		into.ATSLocation = d.ATSLocation
	}
	return nil
}

// fillDetails fetches every posting's body, bounded and concurrent.
//
// Failures are skipped rather than fatal: one unreadable posting must not lose
// the board. A posting with no body still ingests and scores as an honest
// abstention, and the next poll retries it.
func (a *Adapter) fillDetails(ctx context.Context, src source.Source, jobs []wireJob) {
	work := make(chan int)
	var wg sync.WaitGroup

	for i := 0; i < detailConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				j := &jobs[idx]
				resp, err := source.Do(ctx, a.client, a.Vendor(), source.Source{}, source.Request{
					URL: fmt.Sprintf("https://%s.bamboohr.com/careers/%s/detail",
						src.BoardToken, j.ID),
					UserAgent: browserUA,
					MaxBody:   maxBodyBytes,
				})
				if err != nil || len(resp.Body) == 0 {
					continue
				}
				_ = a.ParseDetail(resp.Body, j)
			}
		}()
	}

	for i := range jobs {
		select {
		case work <- i:
		case <-ctx.Done():
			close(work)
			wg.Wait()
			return
		}
	}
	close(work)
	wg.Wait()
}

func (a *Adapter) convert(j *wireJob, boardToken string) (source.RawPosting, error) {
	if j.ID == "" || strings.TrimSpace(j.Name) == "" {
		return source.RawPosting{}, fmt.Errorf("bamboohr: job missing id or name: %w", source.ErrMalformed)
	}
	raw, _ := json.Marshal(j)

	url := j.ShareURL
	if url == "" && boardToken != "" {
		url = fmt.Sprintf("https://%s.bamboohr.com/careers/%s", boardToken, j.ID)
	}

	p := source.RawPosting{
		ExternalID:      j.ID,
		Title:           strings.TrimSpace(j.Name),
		LocationRaw:     location(j),
		DescriptionHTML: j.Description,
		ApplyURL:        url,
		PostingURL:      url,
		Department:      strings.TrimSpace(j.DepartmentLabel),
		EmploymentType:  source.NormaliseEmployment(j.EmploymentType),
		Raw:             raw,
	}
	if j.IsRemote != nil && *j.IsRemote {
		p.WorkplaceType = "remote"
	}
	// datePosted is a DATE with no time, so it is parsed as midnight UTC and
	// flagged an estimate — the same rounding Workable gets, and for the same
	// reason: it makes a posting look OLDER, never fresher.
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(j.DatePosted)); err == nil {
		p.PostedAt = &t
		p.PostedAtIsEstimate = true
	}
	return p, nil
}

// location joins whichever of the two location objects carries anything.
//
// BambooHR sends both `location` and `atsLocation`, and on flyio's board every
// field of both is null — a fully remote company that states no place. That is a
// real state, not a parse failure, and it produces an empty string rather than a
// fabricated one.
func location(j *wireJob) string {
	for _, l := range []*wireLoc{j.Location, j.ATSLocation} {
		if l == nil {
			continue
		}
		parts := nonEmpty(l.City, l.State, source.FirstNonEmpty(l.Country, l.AddressCountry))
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
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
