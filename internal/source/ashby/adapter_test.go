package ashby

import (
	"os"
	"strings"
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

// Golden fixtures are captured real responses from public Ashby boards
// (browserbase and supabase, 2026-08-19), trimmed to the cases the adapter
// branches on. Never a live call in a test: an adapter suite that depends on
// someone else's API is a suite that goes red when they deploy.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func parse(t *testing.T, name string) []source.RawPosting {
	t.Helper()
	got, err := New(nil, "test").Parse(fixture(t, name))
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return got
}

func find(t *testing.T, posts []source.RawPosting, titleFrag string) source.RawPosting {
	t.Helper()
	for _, p := range posts {
		if strings.Contains(p.Title, titleFrag) {
			return p
		}
	}
	t.Fatalf("no posting whose title contains %q", titleFrag)
	return source.RawPosting{}
}

func TestParse_MapsABoard(t *testing.T) {
	got := parse(t, "board-full.json")
	if len(got) == 0 {
		t.Fatal("no postings parsed")
	}

	p := find(t, got, "Core Infrastructure")
	if p.ExternalID == "" {
		t.Error("ExternalID is empty — dedup and upsert both key on it")
	}
	if p.LocationRaw == "" {
		t.Error("LocationRaw is empty")
	}
	if p.DescriptionHTML == "" {
		t.Error("DescriptionHTML is empty; Ashby serves bodies in the list response")
	}
	if p.ApplyURL == "" {
		t.Error("ApplyURL is empty — the apply link is the product's whole output")
	}
	// publishedAt is a genuine first-publication timestamp, unlike Greenhouse's
	// list endpoint. Freshness is real here rather than an upper bound.
	if p.PostedAt == nil {
		t.Error("PostedAt is nil; publishedAt should have parsed")
	}
	if p.PostedAtIsEstimate {
		t.Error("PostedAtIsEstimate is true, but publishedAt is a real date")
	}
	if p.EmploymentType != "full_time" {
		t.Errorf("EmploymentType = %q, want full_time (from FullTime)", p.EmploymentType)
	}
}

// An hourly rate presented as an annual salary is a fabricated number, and it
// reached production: three live Ashby postings showed $30–45 and $62–108 "per
// year". Ashby sends the period as `interval: "1 HOUR"`; the struct read a key
// Ashby never sends, so every rate defaulted to yearly.
func TestParse_HourlyRateKeepsItsPeriod(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Business Development Intern")

	if p.CompMax == nil {
		t.Fatal("CompMax is nil; the fixture publishes an hourly range")
	}
	if *p.CompMax > 1000 && p.CompPeriod == "year" {
		t.Fatalf("CompMax %.2f/year is plausible; the fixture should be an hourly rate", *p.CompMax)
	}
	if p.CompPeriod != "hour" {
		t.Errorf("CompPeriod = %q for a rate of %.2f, want hour — "+
			"%.2f per year is not a salary anyone is offered", p.CompPeriod, *p.CompMax, *p.CompMax)
	}
}

func TestParse_SalaryIsAnnualWhenTheVendorSaysSo(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Core Infrastructure")

	if p.CompMin == nil || p.CompMax == nil {
		t.Fatal("compensation was not read; the fixture publishes a salary band")
	}
	if p.CompPeriod != "year" {
		t.Errorf("CompPeriod = %q, want year", p.CompPeriod)
	}
	if p.CompCurrency != "USD" {
		t.Errorf("CompCurrency = %q, want USD", p.CompCurrency)
	}
	if !p.CompIsStructured {
		t.Error("CompIsStructured is false for a vendor-published band")
	}
}

// Equity and commission are not salary. Including them produces a number that
// means nothing and compares to nothing.
func TestParse_EquityAndCommissionAreNotSalary(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Account Executive")

	if p.CompMin == nil {
		t.Fatal("the Salary component should still have been read")
	}
	// The fixture's commission components carry no values; if they were being
	// counted, the floor would collapse to them.
	if *p.CompMin < 1000 {
		t.Errorf("CompMin = %.2f — a commission component looks to have been counted as salary", *p.CompMin)
	}
}

func TestParse_NoPublishedCompensationClaimsNothing(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Design Engineer")

	if p.CompMin != nil || p.CompMax != nil {
		t.Errorf("invented a salary band (%v–%v) for a posting that publishes none", p.CompMin, p.CompMax)
	}
	if p.CompPeriod != "" {
		t.Errorf("CompPeriod = %q with no compensation to describe", p.CompPeriod)
	}
}

// isListed=false means the employer has unpublished it. Ingesting it would show
// a role nobody can apply to — the ghost-job problem this product exists to
// reduce.
func TestParse_UnlistedJobsAreDropped(t *testing.T) {
	got := parse(t, "board-unlisted.json")
	if len(got) != 1 {
		t.Fatalf("parsed %d postings, want 1 — the unlisted one should be dropped", len(got))
	}
	if strings.Contains(got[0].Title, "Product Designer") {
		t.Error("the unlisted posting was ingested")
	}
}

func TestParse_SecondaryLocationsAreJoinedNotDiscarded(t *testing.T) {
	got := parse(t, "board-secondary-locations.json")
	if len(got) != 1 {
		t.Fatalf("parsed %d postings, want 1", len(got))
	}
	if !strings.Contains(got[0].LocationRaw, "/") {
		t.Errorf("LocationRaw = %q; secondary locations should be joined, "+
			"because the user always sees the original string", got[0].LocationRaw)
	}
}

func TestParse_EmptyBoardIsNotAnError(t *testing.T) {
	got := parse(t, "board-empty.json")
	if len(got) != 0 {
		t.Errorf("parsed %d postings from an empty board", len(got))
	}
}

// A board we cannot decode must be an error, not an empty result: an empty
// result makes ReconcileAbsent close every posting the company has.
func TestParse_MalformedBodyIsAnErrorNotAnEmptyBoard(t *testing.T) {
	if _, err := New(nil, "test").Parse(fixture(t, "board-malformed.json")); err == nil {
		t.Fatal("malformed JSON parsed without error; absent postings would be closed")
	}
}
