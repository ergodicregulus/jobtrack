// Package source fetches job postings from public ATS feeds.
//
// One adapter per vendor, all behind the Adapter interface. This is the only
// vendor-aware surface in the system: everything downstream consumes
// RawPosting and cannot tell which ATS produced it.
//
// That boundary is what keeps ingestion tractable across seven vendors with
// seven different schemas, date formats and compensation shapes. When a vendor
// changes, exactly one package and one golden fixture change.
package source

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Vendor identifies an ATS. Values match the source_vendor enum in the schema.
type Vendor string

const (
	VendorGreenhouse      Vendor = "greenhouse"
	VendorLever           Vendor = "lever"
	VendorAshby           Vendor = "ashby"
	VendorSmartRecruiters Vendor = "smartrecruiters"
	VendorRecruitee       Vendor = "recruitee"
	VendorWorkable        Vendor = "workable"
	VendorWorkday         Vendor = "workday"
	VendorPersonio        Vendor = "personio"
	VendorKeka            Vendor = "keka"
	VendorBambooHR        Vendor = "bamboohr"
	VendorJSONLD          Vendor = "jsonld"
)

// Source is one company's feed on one vendor.
type Source struct {
	ID         int64
	CompanyID  int64
	Vendor     Vendor
	BoardToken string

	// Conditional-request state. Both are carried because vendors are
	// inconsistent about which they honour, and a wrongly-assumed 304 costs
	// freshness — which is the product — while an unnecessary 200 costs only
	// bandwidth.
	ETag         string
	LastModified string
	ContentHash  []byte

	// DetailCursor is how far a two-phase adapter got through the board last
	// time. Vendors that serve descriptions in the list ignore it.
	//
	// It exists because a bounded detail phase with no memory is not a bounded
	// detail phase — it is the same first N postings, every poll, forever.
	DetailCursor int
}

// RawPosting is a posting as the vendor described it, mapped onto one shape but
// not yet normalised.
//
// Fields are deliberately close to the wire format. Interpretation — parsing
// "3+ years" into a range, deciding whether "remote" is true — happens in the
// normalise package, so that adapters stay mechanical and testable against
// captured responses.
type RawPosting struct {
	// ExternalID is the vendor's stable identifier, or a derived hash where the
	// vendor provides none. The derivation rule is documented per vendor in
	// docs/research/source-catalog.md.
	ExternalID string

	// RequisitionID, where exposed, is a free dedup key: two postings from one
	// company sharing it are the same role, with no similarity computation.
	RequisitionID string

	Title       string
	LocationRaw string

	// DescriptionHTML is sanitised downstream, never here. Adapters do not get
	// to decide what is safe to render.
	DescriptionHTML string

	ApplyURL   string
	PostingURL string

	Department string
	Office     string

	// PostedAt should be the vendor's *first published* timestamp where one
	// exists. Falling back to an updated-at makes an edited 40-day-old posting
	// look fresh, which corrupts the signal the whole product is built on.
	PostedAt *time.Time
	// PostedAtIsEstimate records that PostedAt is a fallback rather than a
	// first-publication date, so the UI can present age as an upper bound.
	PostedAtIsEstimate bool

	// Compensation, when the vendor exposes it structurally. Nil means not
	// disclosed, which is distinct from zero and must stay distinguishable all
	// the way to the UI.
	CompMin          *float64
	CompMax          *float64
	CompCurrency     string
	CompPeriod       string
	CompIsStructured bool

	// WorkplaceType is the vendor's own claim. Treated as a hint, never as
	// truth: a posting tagged "remote" whose body says "3 days in office" is
	// hybrid, and the description wins.
	WorkplaceType string

	EmploymentType string

	// AI screening disclosure, currently Greenhouse-only. Nil means the vendor
	// does not expose it — which is "unknown", never "no AI".
	AIScreeningDisclosed *bool
	AIDisclaimer         string
	AIOptOutURL          string

	// Raw is the vendor payload, retained so a normalisation fix can be
	// reapplied without re-fetching every board.
	Raw []byte
}

// FetchResult is the outcome of one poll.
type FetchResult struct {
	// NotModified is true when the server returned 304 or the body hashed
	// identically. This is the common case — roughly 90% of polls at steady
	// state — and it is what makes 2-hourly polling of watched companies
	// affordable.
	NotModified bool

	Postings []RawPosting

	// New validators to store for the next conditional request.
	ETag         string
	LastModified string
	ContentHash  []byte

	// DetailCursor is where the next poll's detail phase should resume. A value
	// at or beyond the board's length means the board has been swept once, and
	// is what allows the unchanged-board short-circuit to fire safely.
	DetailCursor int

	StatusCode int
	// RetryAfter is honoured exactly when the server sends it.
	RetryAfter time.Duration
}

// Adapter turns a vendor's feed into RawPostings.
//
// Implementations must be pure with respect to the network: Parse takes bytes,
// so every adapter is testable against captured responses with no live ATS
// involved. That property is what allows golden-file testing, and golden-file
// testing is what makes a seven-vendor ingestion layer maintainable.
type Adapter interface {
	Vendor() Vendor

	// Fetch performs a conditional request and parses the result.
	Fetch(ctx context.Context, src Source) (FetchResult, error)

	// Parse converts a response body into postings. Separate from Fetch so it
	// can be exercised directly by golden tests.
	Parse(body []byte) ([]RawPosting, error)
}

// Common failures. Callers distinguish these because the response differs:
// a 404 disables a source, a 429 backs off, a parse failure alerts.
var (
	// ErrSourceGone means the board no longer exists (404, or DNS failure for
	// subdomain-based vendors like Recruitee).
	ErrSourceGone = errors.New("source: board no longer exists")

	// ErrRateLimited means the vendor asked us to slow down.
	ErrRateLimited = errors.New("source: rate limited")

	// ErrMalformed means the response did not parse. The raw body is retained
	// and the job fails loudly — silently dropping it is how a vendor schema
	// change hides for a month.
	ErrMalformed = errors.New("source: malformed response")

	// ErrSuspiciousEmpty means a board that had postings returned none.
	// Treated as a fetch failure rather than a mass closure: this guard has
	// saved every aggregator that implemented it and embarrassed every one
	// that did not.
	ErrSuspiciousEmpty = errors.New("source: unexpectedly empty response")
)

// HTTPDoer is the subset of http.Client adapters use, so tests can substitute
// a transport without a live server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}
