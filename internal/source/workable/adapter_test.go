package workable

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

// Captured from the public skroutz account on 2026-08-25 with details=true.
// Never a live call in a test.
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

	p := find(t, got, "BI Engineer")
	if p.ExternalID != "CFF550F9FE" {
		t.Errorf("ExternalID = %q, want the shortcode", p.ExternalID)
	}
	// The employer's own requisition reference is a free dedup key.
	if p.RequisitionID != "BE0826" {
		t.Errorf("RequisitionID = %q", p.RequisitionID)
	}
	if !strings.Contains(p.LocationRaw, "Athens") {
		t.Errorf("LocationRaw = %q", p.LocationRaw)
	}
	if p.EmploymentType != "full_time" {
		t.Errorf("EmploymentType = %q", p.EmploymentType)
	}
}

// details=true is the only reason this is a one-request adapter. Without the
// parameter every posting comes back with no description at all, and the board
// would need a detail fetch per role.
func TestParse_CarriesTheDescription(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "BI Engineer")
	if len(p.DescriptionHTML) < 1000 {
		t.Errorf("description is %d bytes — details=true may have been dropped", len(p.DescriptionHTML))
	}
}

// published_on is a date with no time, so it is parsed as midnight UTC and
// flagged an estimate. The rounding direction is the point: this makes a
// posting look up to 24h OLDER, never fresher. An adapter that rounds age
// downward manufactures freshness.
func TestParse_DateOnlyPostedAtIsFlaggedAndRoundsOlder(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "BI Engineer")
	if p.PostedAt == nil {
		t.Fatal("PostedAt is nil")
	}
	if !p.PostedAtIsEstimate {
		t.Error("a date-only timestamp must be flagged as an estimate")
	}
	if h, m := p.PostedAt.Hour(), p.PostedAt.Minute(); h != 0 || m != 0 {
		t.Errorf("PostedAt = %v, want midnight", p.PostedAt)
	}
	if y, mo, d := p.PostedAt.Date(); y != 2026 || mo != 8 || d != 14 {
		t.Errorf("PostedAt = %v, want 2026-08-14", p.PostedAt)
	}
}

// An unmapped employment type must be empty, not guessed. skroutz's first
// posting genuinely sends "".
func TestParse_UnknownEmploymentTypeStaysEmpty(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Automation Senior Manager")
	if p.EmploymentType != "" {
		t.Errorf("EmploymentType = %q, want empty for an unstated type", p.EmploymentType)
	}
}

// The flat country/city pair only ever holds the first location, so a role open
// in two cities would look open in one.
func TestParse_PrefersTheStructuredLocations(t *testing.T) {
	body := []byte(`{"jobs":[{"shortcode":"X","title":"Eng","city":"Athens","country":"Greece",
	  "locations":[{"city":"Athens","country":"Greece"},{"city":"Berlin","country":"Germany"}]}]}`)
	got, err := New(nil, "test").Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.Contains(got[0].LocationRaw, "Berlin") {
		t.Errorf("LocationRaw = %q, second location dropped", got[0].LocationRaw)
	}
}

func TestParse_SkipsJobsWithNoShortcode(t *testing.T) {
	got, err := New(nil, "test").Parse([]byte(`{"jobs":[{"title":"No id"},{"shortcode":"A","title":"Fine"}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Fine" {
		t.Errorf("got %+v", got)
	}
}

func TestParse_RejectsMalformed(t *testing.T) {
	if _, err := New(nil, "test").Parse([]byte(`{"jobs":`)); err == nil {
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
