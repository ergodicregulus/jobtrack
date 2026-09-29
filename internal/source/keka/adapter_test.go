package keka

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

// Captured from the public spyneai board on 2026-09-01, trimmed to one job per
// shape the adapter branches on. Never a live call in a test.
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
	if len(got) != 7 {
		t.Fatalf("got %d postings, want 7", len(got))
	}

	p := find(t, got, "Infosec Engineer")
	if p.ExternalID == "" {
		t.Error("ExternalID is empty")
	}
	if p.Department == "" {
		t.Error("Department is empty")
	}
	if len(p.DescriptionHTML) < 500 {
		t.Errorf("description is %d bytes, suspiciously short", len(p.DescriptionHTML))
	}
	// publishedOn carries a real time of day, which is what lets Keka postings
	// feed the ingest-latency measurement.
	if p.PostedAt == nil {
		t.Fatal("PostedAt is nil")
	}
	if p.PostedAt.Hour() == 0 && p.PostedAt.Minute() == 0 {
		t.Error("PostedAt looks date-only; Keka publishes a timestamp")
	}
	if p.PostedAtIsEstimate {
		t.Error("publishedOn is exact, not a fallback")
	}
}

// The two integer enums are not decoded, and must not be guessed at.
//
// jobType was 2 for every posting on the sampled board — an intern, a product
// manager, an analyst and an engineer alike — so it does not mean employment
// type on this evidence. salaryPeriod took 0 and 4 over figures of the same
// magnitude, so it cannot be read as year/month. Emitting either would be the
// Ashby interval bug again: "$30 - $45 per year" for an hourly contract.
func TestParse_DoesNotGuessTheUndecodedEnums(t *testing.T) {
	for _, p := range parse(t, "board-full.json") {
		if p.EmploymentType != "" {
			t.Errorf("%s: EmploymentType = %q, but jobType is not decoded",
				p.Title, p.EmploymentType)
		}
		if p.CompIsStructured || p.CompMin != nil || p.CompMax != nil || p.CompPeriod != "" {
			t.Errorf("%s: emitted compensation from an undecoded salaryPeriod", p.Title)
		}
	}
}

// A role open in two cities must not look open in one.
func TestParse_KeepsEveryLocation(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Director - Engineering")
	if !strings.Contains(p.LocationRaw, "·") {
		t.Errorf("LocationRaw = %q, second location dropped", p.LocationRaw)
	}
}

// A posting with no location at all must still ingest — it is a real state on
// this board, not a parse failure.
func TestParse_SurvivesAPostingWithNoLocation(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "SDE Intern")
	if p.LocationRaw != "" {
		t.Errorf("LocationRaw = %q, want empty", p.LocationRaw)
	}
	if p.ExternalID == "" {
		t.Error("a posting with no location was dropped")
	}
}

// The employer's own skill list is better evidence than anything extraction can
// recover from prose, and it reaches posting_skills through the existing
// vocabulary rather than a second path.
func TestParse_AppendsTheEmployersSkills(t *testing.T) {
	p := find(t, parse(t, "board-full.json"), "Account Executive")
	if !strings.Contains(p.DescriptionHTML, "<h2>Skills</h2>") {
		t.Error("skillNames not appended to the description")
	}
}

// The feed carries no links, so both URLs are ours to construct.
func TestSetURLs_BuildsFromTheTenant(t *testing.T) {
	p := source.RawPosting{ExternalID: "157263"}
	setURLs(&p, "spyneai")
	if p.PostingURL != "https://spyneai.keka.com/careers/jobdetails/157263" {
		t.Errorf("PostingURL = %q", p.PostingURL)
	}
	if p.ApplyURL == "" {
		t.Error("ApplyURL is empty")
	}
}

// The org id is a per-tenant GUID that cannot be derived from the company name.
func TestSplitToken(t *testing.T) {
	portal, org, err := splitToken("default/49556835-6902-4481-b4e9-11910edf9cb1")
	if err != nil || portal != "default" || org != "49556835-6902-4481-b4e9-11910edf9cb1" {
		t.Errorf("two-part: %q %q %v", portal, org, err)
	}
	if portal, _, err := splitToken("49556835-6902"); err != nil || portal != defaultPortal {
		t.Errorf("bare org id should assume the default portal: %q %v", portal, err)
	}
	if _, _, err := splitToken(""); err == nil {
		t.Error("empty token should error")
	}
	if _, _, err := splitToken("a/b/c"); err == nil {
		t.Error("three-part token should error")
	}
}

func TestParse_RawIsValidJSON(t *testing.T) {
	for _, p := range parse(t, "board-full.json") {
		if !json.Valid(p.Raw) {
			t.Errorf("%s: Raw is not valid JSON", p.ExternalID)
		}
	}
}

func TestParse_RejectsMalformed(t *testing.T) {
	if _, err := New(nil, "test").Parse([]byte(`[{"id":1,`)); err == nil {
		t.Fatal("want an error for truncated JSON, got nil")
	}
}
