// Package workday adapts the Workday CXS job-board endpoint.
//
//	POST https://{tenant}.{dc}.myworkdayjobs.com/wday/cxs/{tenant}/{site}/jobs
//	GET  https://{tenant}.{dc}.myworkdayjobs.com/wday/cxs/{tenant}/{site}{externalPath}
//
// Verified by direct call on 2026-08-25 against NVIDIA. See
// docs/research/source-catalog.md#workday.
//
// TIER 1b, NOT TIER 1. This is an undocumented internal API, not a published
// contract like Greenhouse's boards API. ADR-0004's durability argument still
// holds — the endpoint IS the customer's own careers page, so the vendor's
// paying customer depends on it working — but it can change without notice and
// this adapter is written expecting that.
//
// Four properties of the API drive almost all of the code below.
//
//  1. `total` IS CAPPED AT 2,000 AND LIES. Measured on NVIDIA: `total` reports
//     2,000 while the facet counts sum to 2,630. An adapter that trusts `total`
//     stops 24% short and reports success, which is precisely how BoschGroup
//     was truncated to 41% of its board for a week. Coverage is therefore
//     derived from the FACETS, and the board is walked per facet value.
//
//  2. The list gives a relative date. `postedOn` is "Posted Today", "Posted
//     Yesterday", "Posted 30+ Days Ago" — a string, in the tenant's locale,
//     with a ceiling at 30. The detail endpoint carries `startDate` as a real
//     ISO date, and that is the only field worth trusting for posting age.
//
//  3. Descriptions live only on the detail endpoint, so this is a two-phase
//     vendor and uses DetailCursor the same way SmartRecruiters does.
//
//  4. The data-centre segment (wd1, wd3, wd5…) is part of the tenant's URL and
//     is not derivable from the tenant name. It is carried in the board token.
package workday

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

const (
	// pageSize is what one list call returns. Workday accepts larger values and
	// ignores them above 20, so asking for more only looks like it works.
	pageSize = 20

	// capTotal is the value `total` saturates at. Reaching it is the signal
	// that the response is truncated, never that the board holds exactly this
	// many.
	capTotal = 2000

	// maxPagesPerSlice bounds one facet slice.
	//
	// 110, not 60. At 60 this silently reintroduced the bug it exists to
	// prevent: 60 x 20 = 1,200 rows per slice, and NVIDIA's largest facet value
	// (Engineering, 1,794) was cut short. The live probe returned 2,037 against
	// a facet-derived true total of 2,630 — beating the vendor's cap and then
	// truncating on our own.
	//
	// A slice cannot exceed the vendor's own 2,000 cap, so 100 pages covers any
	// slice by construction; the extra ten are headroom for a cap that moves.
	maxPagesPerSlice = 110

	// detailsPerPoll bounds the second phase. Descriptions fill in over
	// successive polls rather than in one long run that would hold a worker
	// slot and hammer one host.
	detailsPerPoll = 40
)

// Adapter reads a Workday careers site.
type Adapter struct {
	client    source.HTTPDoer
	userAgent string
}

// New returns a Workday adapter.
func New(client source.HTTPDoer, userAgent string) *Adapter {
	return &Adapter{client: client, userAgent: userAgent}
}

// Vendor identifies this adapter.
func (a *Adapter) Vendor() source.Vendor { return source.VendorWorkday }

// --- wire types -------------------------------------------------------------

type wireList struct {
	Total       int         `json:"total"`
	JobPostings []wireJob   `json:"jobPostings"`
	Facets      []wireFacet `json:"facets"`
}

type wireJob struct {
	Title         string `json:"title"`
	ExternalPath  string `json:"externalPath"`
	LocationsText string `json:"locationsText"`
	PostedOn      string `json:"postedOn"`
	// BulletFields carries the requisition id, and is the only place the list
	// response exposes it.
	BulletFields []string `json:"bulletFields"`
}

type wireFacet struct {
	FacetParameter string       `json:"facetParameter"`
	Descriptor     string       `json:"descriptor"`
	Values         []wireFacetV `json:"values"`
}

type wireFacetV struct {
	ID         string `json:"id"`
	Descriptor string `json:"descriptor"`
	Count      int    `json:"count"`
}

type wireDetail struct {
	JobPostingInfo struct {
		Title          string `json:"title"`
		JobDescription string `json:"jobDescription"`
		ExternalURL    string `json:"externalUrl"`
		JobReqID       string `json:"jobReqId"`
		Country        struct {
			Descriptor string `json:"descriptor"`
		} `json:"country"`
		Location string `json:"location"`
		// StartDate is ISO (2026-08-25) and is the only trustworthy date here.
		StartDate string `json:"startDate"`
		TimeType  string `json:"timeType"`
	} `json:"jobPostingInfo"`
}

// --- parsing ----------------------------------------------------------------

// Parse converts one list response into postings.
//
// Separate from Fetch so golden tests can drive it directly, which is the whole
// reason the interface splits them.
func (a *Adapter) Parse(body []byte) ([]source.RawPosting, error) {
	var list wireList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("workday: decode list: %w", err)
	}

	out := make([]source.RawPosting, 0, len(list.JobPostings))
	for i := range list.JobPostings {
		p, err := convert(&list.JobPostings[i])
		if err != nil {
			// One unparseable row must not discard the board.
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// trueTotal reports how many postings the board actually holds.
//
// `total` is capped at 2,000 and cannot be trusted at that value. Facet counts
// are not capped, so the largest facet group's sum is a better floor: every
// posting appears in exactly one value of any given facet, so each group's sum
// is the board size seen from a different angle.
func trueTotal(list *wireList) (int, bool) {
	best := 0
	for _, f := range list.Facets {
		sum := 0
		for _, v := range f.Values {
			sum += v.Count
		}
		if sum > best {
			best = sum
		}
	}
	if best == 0 {
		return 0, false
	}
	return best, true
}

// IsTruncated reports whether this response is hiding postings.
//
// Two independent signals, because either alone misses cases: `total` sitting
// exactly on the cap, and the facets describing more postings than `total`
// admits.
func IsTruncated(body []byte) bool {
	var list wireList
	if err := json.Unmarshal(body, &list); err != nil {
		return false
	}
	if actual, ok := trueTotal(&list); ok && actual > list.Total {
		return true
	}
	return list.Total >= capTotal
}

// sliceFacet picks the facet to walk the board by.
//
// The one with the most values, because it cuts the board into the smallest
// pieces and is therefore likeliest to get every slice under the cap. Location
// would be finer still but Workday nests it under a group with no counts, so
// its slices cannot be checked for truncation — a facet whose coverage cannot
// be verified is worse than a coarser one that can.
func sliceFacet(list *wireList) (string, []wireFacetV, bool) {
	var name string
	var vals []wireFacetV
	for _, f := range list.Facets {
		if f.FacetParameter == "" || len(f.Values) <= 1 {
			continue
		}
		// Skip groups whose values carry no count: they cannot be verified.
		if f.Values[0].Count == 0 {
			continue
		}
		if len(f.Values) > len(vals) {
			name, vals = f.FacetParameter, f.Values
		}
	}
	if name == "" {
		return "", nil, false
	}
	return name, vals, true
}

func convert(j *wireJob) (source.RawPosting, error) {
	if j.ExternalPath == "" || j.Title == "" {
		return source.RawPosting{}, fmt.Errorf("workday: posting missing path or title")
	}

	raw, _ := json.Marshal(j)
	p := source.RawPosting{
		// The path is stable and unique per requisition, and unlike the
		// requisition id it is always present on the list response.
		ExternalID:    j.ExternalPath,
		RequisitionID: firstBullet(j.BulletFields),
		Title:         strings.TrimSpace(j.Title),
		LocationRaw:   strings.TrimSpace(j.LocationsText),
		Raw:           raw,
	}

	// PostedAt is deliberately NOT set from `postedOn`. It is relative text
	// with a 30-day ceiling — "Posted 30+ Days Ago" is every posting older than
	// a month — so deriving a date from it would manufacture precision the
	// source does not have. The detail phase sets it from `startDate`.
	return p, nil
}

func firstBullet(b []string) string {
	if len(b) == 0 {
		return ""
	}
	return strings.TrimSpace(b[0])
}

// applyDetail fills in what only the detail endpoint carries.
func applyDetail(p *source.RawPosting, d *wireDetail) {
	info := d.JobPostingInfo
	if info.JobDescription != "" {
		p.DescriptionHTML = info.JobDescription
	}
	if info.ExternalURL != "" {
		p.ApplyURL = info.ExternalURL
		p.PostingURL = info.ExternalURL
	}
	if info.JobReqID != "" {
		p.RequisitionID = info.JobReqID
	}
	if info.Location != "" && p.LocationRaw == "" {
		p.LocationRaw = info.Location
	}
	if info.TimeType != "" {
		p.EmploymentType = info.TimeType
	}
	// The only date worth trusting. Parsed as a plain day, so a posting is
	// never claimed to be fresher than the source actually states.
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(info.StartDate)); err == nil {
		p.PostedAt = &t
	}
}

// --- fetching ---------------------------------------------------------------

// Fetch reads a whole board, walking facets when the cap hides postings.
//
// The board token carries the URL parts that are not derivable from each other:
//
//	{datacentre}/{tenant}/{site}    e.g. wd5/nvidia/nvidiaexternalcareersite
//
// Phase one lists. If the first response is truncated, the board is re-walked
// one facet value at a time and the results merged by external path, because
// the same posting appears in several slices only if the facet is not a
// partition — and merging is cheaper than assuming it is.
//
// Phase two fills descriptions, bounded by detailsPerPoll and resumed from
// DetailCursor. Without the cursor a bounded detail phase is not bounded work:
// it is the same first N postings, every poll, forever.
func (a *Adapter) Fetch(ctx context.Context, src source.Source) (source.FetchResult, error) {
	dc, tenant, site, err := splitToken(src.BoardToken)
	if err != nil {
		return source.FetchResult{}, err
	}
	base := fmt.Sprintf("https://%s.%s.myworkdayjobs.com/wday/cxs/%s/%s", tenant, dc, tenant, site)

	var result source.FetchResult
	hash := sha256.New()
	seen := map[string]source.RawPosting{}
	order := []string{}

	first, res, err := a.list(ctx, base, nil, 0, src)
	if err != nil {
		return source.FetchResult{}, err
	}
	result.StatusCode = res.StatusCode
	result.ETag = res.Header.Get("ETag")
	result.LastModified = res.Header.Get("Last-Modified")

	// A byte-identical board is the common case and costs one request.
	if res.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	hash.Write(first)

	var head wireList
	if err := json.Unmarshal(first, &head); err != nil {
		return source.FetchResult{}, fmt.Errorf("workday: decode first page: %w", err)
	}

	for _, facets := range walkPlan(first, &head) {
		if err := a.walkSlice(ctx, base, facets, first, src, hash, seen, &order); err != nil {
			return source.FetchResult{}, err
		}
	}

	postings := make([]source.RawPosting, 0, len(order))
	for _, id := range order {
		postings = append(postings, seen[id])
	}

	cursor := a.fillDescriptions(ctx, base, postings, src.DetailCursor, src)

	result.Postings = postings
	result.ContentHash = hash.Sum(nil)
	result.DetailCursor = cursor
	return result, nil
}

// walkPlan decides how the board will be read.
//
// One unfacetted pass when the first response is complete; one pass per facet
// value when it is not. Trusting `total` here is the mistake that truncated
// BoschGroup to 41% of its board.
func walkPlan(first []byte, head *wireList) []map[string][]string {
	if !IsTruncated(first) {
		return []map[string][]string{nil}
	}
	name, values, ok := sliceFacet(head)
	if !ok {
		// Nothing countable to slice by. One pass is all that can be justified;
		// claiming coverage we cannot verify would be worse than under-reading.
		return []map[string][]string{nil}
	}
	out := make([]map[string][]string, 0, len(values))
	for _, v := range values {
		out = append(out, map[string][]string{name: {v.ID}})
	}
	return out
}

// walkSlice pages through one slice, merging into seen/order.
//
// Merging by external path rather than assuming the facet partitions the board:
// a posting in two slices is cheaper to deduplicate than to reason about.
func (a *Adapter) walkSlice(
	ctx context.Context,
	base string,
	facets map[string][]string,
	first []byte,
	src source.Source,
	hash io.Writer,
	seen map[string]source.RawPosting,
	order *[]string,
) error {
	for page := 0; page < maxPagesPerSlice; page++ {
		body := first
		if page > 0 || facets != nil {
			var err error
			body, _, err = a.list(ctx, base, facets, page*pageSize, src)
			if err != nil {
				return err
			}
			hash.Write(body)
		}

		batch, err := a.Parse(body)
		if err != nil {
			return err
		}
		for _, p := range batch {
			if _, dup := seen[p.ExternalID]; !dup {
				*order = append(*order, p.ExternalID)
			}
			seen[p.ExternalID] = p
		}
		if len(batch) < pageSize {
			return nil
		}
	}
	return nil
}

// splitToken parses `{datacentre}/{tenant}/{site}`.
//
// Three parts because none is derivable from the others: the data-centre
// segment (wd1, wd3, wd5…) is assigned per tenant, and the site name is chosen
// by the customer and is frequently unrelated to the tenant.
func splitToken(token string) (dc, tenant, site string, err error) {
	parts := strings.Split(strings.Trim(token, "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf(
			"workday: board token %q must be {datacentre}/{tenant}/{site}, "+
				"e.g. wd5/nvidia/nvidiaexternalcareersite", token)
	}
	return parts[0], parts[1], parts[2], nil
}

func (a *Adapter) list(
	ctx context.Context,
	base string,
	facets map[string][]string,
	offset int,
	src source.Source,
) ([]byte, *http.Response, error) {
	if facets == nil {
		facets = map[string][]string{}
	}
	payload, err := json.Marshal(map[string]any{
		"appliedFacets": facets,
		"limit":         pageSize,
		"offset":        offset,
		"searchText":    "",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("workday: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/jobs", bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.userAgent)
	// Conditional only on the first, unfacetted call: a POST body that differs
	// per slice makes a validator from another slice meaningless.
	if offset == 0 && len(facets) == 0 && src.ETag != "" {
		req.Header.Set("If-None-Match", src.ETag)
	}

	res, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("workday: list: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotModified {
		return nil, res, nil
	}
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return nil, res, source.ErrRateLimited
	case http.StatusNotFound, http.StatusGone:
		// A site that has been removed is a fact, not a transient failure.
		// Returning a retryable error would have River try it four more times
		// for nothing.
		return nil, res, source.ErrSourceGone
	default:
		return nil, res, fmt.Errorf("workday: %s returned %d", src.BoardToken, res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res, fmt.Errorf("workday: read list: %w", err)
	}
	return body, res, nil
}

// fillDescriptions fetches detail for a bounded window and returns the cursor
// to resume from.
func (a *Adapter) fillDescriptions(
	ctx context.Context,
	base string,
	postings []source.RawPosting,
	start int,
	src source.Source,
) int {
	if len(postings) == 0 {
		return 0
	}
	if start >= len(postings) {
		start = 0
	}

	filled := 0
	i := start
	for ; filled < detailsPerPoll && filled < len(postings); i++ {
		if i >= len(postings) {
			i = 0
		}
		p := &postings[i]
		filled++

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+p.ExternalID, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", a.userAgent)

		res, err := a.client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			// A detail miss is not a board failure. The posting keeps its list
			// fields and is retried on a later poll.
			continue
		}

		var d wireDetail
		if err := json.Unmarshal(body, &d); err != nil {
			continue
		}
		applyDetail(p, &d)
	}

	if i >= len(postings) {
		return 0
	}
	return i
}
