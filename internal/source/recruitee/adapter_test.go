package recruitee

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jobtrack/jobtrack/internal/source"
)

// Captured from the public channable board on 2026-08-25, trimmed to one offer
// per salary/workplace variant. Never a live call in a test.
func parse(t *testing.T, name string) []source.RawPosting {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	got, err := New(nil, "test").Parse(b)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return got
}

func find(t *testing.T, posts []source.RawPosting, frag string) source.RawPosting {
	t.Helper()
	for _, p := range posts {
		if strings.Contains(p.Title, frag) {
			return p
		}
	}
	t.Fatalf("no posting whose title contains %q", frag)
	return source.RawPosting{}
}

func TestParse_MapsABoard(t *testing.T) {
	got := parse(t, "board-full.json")
	if len(got) != 3 {
		t.Fatalf("got %d postings, want 3", len(got))
	}

	p := find(t, got, "Product Manager")
	if p.ExternalID == "" {
		t.Error("ExternalID is empty")
	}
	if !strings.Contains(p.LocationRaw, "Utrecht") {
		t.Errorf("LocationRaw = %q", p.LocationRaw)
	}
	if p.ApplyURL == "" || p.PostingURL == "" {
		t.Errorf("URLs missing: apply=%q posting=%q", p.ApplyURL, p.PostingURL)
	}
	if p.PostedAt == nil {
		t.Fatal("PostedAt is nil")
	}
}

// The period is the whole meaning of the figure. 4,500–6,000 EUR is a monthly
// salary here and an annual one on other boards; converting or dropping the
// period turns a correct number into a fabricated one.
func TestParse_KeepsTheSalaryPeriod(t *testing.T) {
	got := parse(t, "board-full.json")

	monthly := find(t, got, "Product Manager")
	if monthly.CompPeriod != "month" {
		t.Errorf("CompPeriod = %q, want month", monthly.CompPeriod)
	}
	if monthly.CompCurrency != "EUR" {
		t.Errorf("CompCurrency = %q, want EUR", monthly.CompCurrency)
	}
	if monthly.CompMin == nil || *monthly.CompMin != 4500 {
		t.Errorf("CompMin = %v, want 4500", monthly.CompMin)
	}
	if !monthly.CompIsStructured {
		t.Error("CompIsStructured should be true")
	}

	yearly := find(t, got, "Account Executive")
	if yearly.CompPeriod != "year" || yearly.CompCurrency != "USD" {
		t.Errorf("yearly: period=%q currency=%q", yearly.CompPeriod, yearly.CompCurrency)
	}
}

// Recruitee sends an object of four nulls rather than omitting salary, so
// "present" and "disclosed" are different questions. Nil must survive: it means
// undisclosed, which is not zero, all the way to the UI.
func TestParse_UndisclosedSalaryStaysNil(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Open application")
	if p.CompMin != nil || p.CompMax != nil {
		t.Errorf("undisclosed salary produced %v–%v", p.CompMin, p.CompMax)
	}
	if p.CompIsStructured {
		t.Error("CompIsStructured should be false when nothing was disclosed")
	}
}

// published_at, not created_at. The "Open application" evergreen was created in
// 2019 and published in 2021; taking created_at would make it look two years
// fresher, and age is the signal the product is built on.
func TestParse_UsesPublishedAtNotCreatedAt(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Open application")
	if p.PostedAt == nil {
		t.Fatal("PostedAt is nil")
	}
	if got := p.PostedAt.Year(); got != 2021 {
		t.Errorf("PostedAt year = %d, want 2021 (published), not 2019 (created)", got)
	}
	if p.PostedAt.Location().String() != "UTC" {
		t.Errorf("timestamp should be UTC, got %v", p.PostedAt.Location())
	}
}

// The flags are not mutually exclusive — hotelchamp returns hybrid and on_site
// together — so they are read in priority order rather than as a choice.
func TestParse_WorkplaceFlagsArePriorityOrdered(t *testing.T) {
	if p := find(t, parse(t, "board-full.json"), "Product Manager"); p.WorkplaceType != "hybrid" {
		t.Errorf("WorkplaceType = %q, want hybrid", p.WorkplaceType)
	}
}

// Description and requirements arrive as two separate HTML bodies. Both are
// needed: the requirements block is where years-of-experience and skills live.
func TestParse_JoinsDescriptionAndRequirements(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Product Manager")
	if !strings.Contains(p.DescriptionHTML, "<h2>Requirements</h2>") {
		t.Error("requirements section not joined")
	}
}

func TestParse_SkipsUnpublished(t *testing.T) {
	body := []byte(`{"offers":[{"id":1,"title":"Draft","status":"draft"},
	                            {"id":2,"title":"Live","status":"published"}]}`)
	got, err := New(nil, "test").Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Live" {
		t.Errorf("got %+v, want only the published offer", got)
	}
}

func TestParse_RejectsMalformed(t *testing.T) {
	if _, err := New(nil, "test").Parse([]byte(`{"offers":`)); err == nil {
		t.Fatal("want an error for truncated JSON, got nil")
	}
}

// RawPosting.Raw lands in a jsonb column, so it must be valid JSON regardless
// of the wire format the vendor speaks. Personio's feed is XML and its first
// version marshalled Raw back to XML: a valid document that Postgres rejected
// with "invalid input syntax for type json", on every posting, silently
// producing an empty board. Nothing in a golden test touches the database, so
// only a live poll revealed it — this is the assertion that would have.
func TestParse_RawIsValidJSON(t *testing.T) {
	for _, p := range parse(t, "board-full.json") {
		if len(p.Raw) == 0 {
			t.Fatalf("%s: Raw is empty", p.ExternalID)
		}
		if !json.Valid(p.Raw) {
			t.Errorf("%s: Raw is not valid JSON: %.80s", p.ExternalID, p.Raw)
		}
	}
}
