package personio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jobtrack/jobtrack/internal/source"
)

// Captured from the public orderbird feed on 2026-08-25. Never a live call in a
// test: an adapter suite that depends on someone else's API is a suite that goes
// red when they deploy.
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
	got := parse(t, "board-full.xml")
	if len(got) != 5 {
		t.Fatalf("got %d postings, want 5", len(got))
	}

	p := find(t, got, "Account Executive")
	if p.ExternalID != "1935524" {
		t.Errorf("ExternalID = %q", p.ExternalID)
	}
	if p.Department != "Sales" {
		t.Errorf("Department = %q", p.Department)
	}
	if p.Office != "Berlin" {
		t.Errorf("Office = %q", p.Office)
	}
	// createdAt is full RFC 3339 with an offset — one of only three vendors
	// that gives a real time of day.
	if p.PostedAt == nil {
		t.Fatal("PostedAt is nil")
	}
	if y, m := p.PostedAt.Year(), p.PostedAt.Month(); y != 2025 || m != 1 {
		t.Errorf("PostedAt = %v, want Jan 2025", p.PostedAt)
	}
	if p.PostedAtIsEstimate {
		t.Error("PostedAtIsEstimate: createdAt is exact, not a fallback")
	}
}

// A position open in fourteen places must not look open in one. normalise
// reads the whole location string, so dropping the extras filters the role away
// from most of the people it suits.
func TestParse_KeepsAdditionalOffices(t *testing.T) {
	p := find(t, parse(t, "board-full.xml"), "Account Executive")
	for _, want := range []string{"Berlin", "Hamburg", "Köln", "Frankfurt am Main"} {
		if !strings.Contains(p.LocationRaw, want) {
			t.Errorf("LocationRaw %q missing %q", p.LocationRaw, want)
		}
	}
	// Berlin is both the primary office and absent from the extras; it must not
	// be repeated if that ever changes.
	if n := strings.Count(p.LocationRaw, "Berlin"); n != 1 {
		t.Errorf("Berlin appears %d times in %q, want 1", n, p.LocationRaw)
	}
}

// The Werkstudent role is schedule=part-time, employmentType=working_student.
// The student signal is the specific one and the one people filter on.
func TestParse_PrefersTheMoreSpecificEmploymentType(t *testing.T) {
	got := parse(t, "board-full.xml")
	if p := find(t, got, "Werkstudent"); p.EmploymentType != "intern" {
		t.Errorf("Werkstudent EmploymentType = %q, want intern", p.EmploymentType)
	}
	// Where employmentType carries no contract class, the schedule wins.
	if p := find(t, got, "Account Executive"); p.EmploymentType != "full_time" {
		t.Errorf("Account Executive EmploymentType = %q, want full_time", p.EmploymentType)
	}
}

// Sections arrive as {name, value} pairs in the employer's language. The names
// are often the only structure the description has.
func TestParse_JoinsDescriptionSectionsWithHeadings(t *testing.T) {
	p := find(t, parse(t, "board-full.xml"), "Account Executive")
	if !strings.Contains(p.DescriptionHTML, "<h2>Introduction</h2>") {
		t.Error("section headings not emitted")
	}
	if !strings.Contains(p.DescriptionHTML, "Wonach wir suchen") {
		t.Error("later sections dropped")
	}
	if len(p.DescriptionHTML) < 500 {
		t.Errorf("description is %d bytes, suspiciously short", len(p.DescriptionHTML))
	}
}

// The feed has no links of any kind, so both URLs are ours to construct.
func TestSetURLs_BuildsBothFromTheToken(t *testing.T) {
	p := source.RawPosting{ExternalID: "1935524"}
	SetURLs(&p, "orderbird")
	if p.PostingURL != "https://orderbird.jobs.personio.de/job/1935524" {
		t.Errorf("PostingURL = %q", p.PostingURL)
	}
	if !strings.HasPrefix(p.ApplyURL, p.PostingURL) {
		t.Errorf("ApplyURL %q should extend PostingURL", p.ApplyURL)
	}
}

// A tenant that does not exist 307s to the marketing site rather than 404ing,
// so the realistic failure is receiving HTML where a board should be.
//
// The guard is the XMLName field on feed: it makes xml.Unmarshal reject a
// mismatched root instead of returning an empty struct that downstream would
// read as "this company closed every role". This test is what proves the field
// is load-bearing — remove it and this goes red.
func TestParse_RejectsHTMLFromABadTenant(t *testing.T) {
	_, err := New(nil, "test").Parse([]byte(`<html><body><h1>Personio</h1></body></html>`))
	if err == nil {
		t.Fatal("want an error for a non-feed document, got nil")
	}
	if !strings.Contains(err.Error(), "workzag-jobs") {
		t.Errorf("error should name the expected root, got %v", err)
	}
}

func TestParse_RejectsMalformedXML(t *testing.T) {
	if _, err := New(nil, "test").Parse([]byte(`<workzag-jobs><position>`)); err == nil {
		t.Fatal("want an error for truncated XML, got nil")
	}
}

// RawPosting.Raw lands in a jsonb column, so it must be valid JSON regardless
// of the wire format the vendor speaks. Personio's feed is XML and its first
// version marshalled Raw back to XML: a valid document that Postgres rejected
// with "invalid input syntax for type json", on every posting, silently
// producing an empty board. Nothing in a golden test touches the database, so
// only a live poll revealed it — this is the assertion that would have.
func TestParse_RawIsValidJSON(t *testing.T) {
	for _, p := range parse(t, "board-full.xml") {
		if len(p.Raw) == 0 {
			t.Fatalf("%s: Raw is empty", p.ExternalID)
		}
		if !json.Valid(p.Raw) {
			t.Errorf("%s: Raw is not valid JSON: %.80s", p.ExternalID, p.Raw)
		}
	}
}
