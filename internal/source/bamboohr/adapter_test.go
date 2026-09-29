package bamboohr

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

// Captured from the public flyio board on 2026-09-01: the list, plus the detail
// document for each posting in it. Never a live call in a test.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func TestParse_ListAloneHasNoBody(t *testing.T) {
	got, err := New(nil, "test").Parse(fixture(t, "board-full.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d postings, want 4", len(got))
	}
	// This is the fact that makes the adapter two-phase. If BambooHR ever starts
	// sending a description in the list, this test fails and the detail phase
	// becomes optional — which is worth being told about.
	for _, p := range got {
		if p.DescriptionHTML != "" {
			t.Errorf("%s: the list carried a body; the detail phase may be unnecessary now",
				p.ExternalID)
		}
	}
}

// The detail document is where everything that matters lives.
func TestParseDetail_MergesTheBodyOntoTheListEntry(t *testing.T) {
	a := New(nil, "test")
	var lr listResponse
	if err := json.Unmarshal(fixture(t, "board-full.json"), &lr); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	j := &lr.Result[0]
	if err := a.ParseDetail(fixture(t, "detail-"+j.ID+".json"), j); err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}

	p, err := a.convert(j, "flyio")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(p.DescriptionHTML) < 1000 {
		t.Errorf("description is %d bytes; the detail merge did not take", len(p.DescriptionHTML))
	}
	if p.PostedAt == nil {
		t.Fatal("PostedAt is nil after the detail merge")
	}
	// datePosted is a DATE. Parsed as midnight so a posting looks OLDER, never
	// fresher — the rounding direction is the only safe one, and the estimate
	// flag is how the UI knows to present age as an upper bound.
	if !p.PostedAtIsEstimate {
		t.Error("a date-only timestamp must be flagged as an estimate")
	}
	if h, m := p.PostedAt.Hour(), p.PostedAt.Minute(); h != 0 || m != 0 {
		t.Errorf("PostedAt = %v, want midnight", p.PostedAt)
	}
	if p.ApplyURL == "" || !strings.Contains(p.ApplyURL, j.ID) {
		t.Errorf("ApplyURL = %q, want one containing the id", p.ApplyURL)
	}
}

// A board that states no location at all is a real state, not a parse failure.
// Every location field on flyio's board is null — a fully remote company.
func TestConvert_NoLocationProducesNoLocation(t *testing.T) {
	a := New(nil, "test")
	var lr listResponse
	if err := json.Unmarshal(fixture(t, "board-full.json"), &lr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p, err := a.convert(&lr.Result[0], "flyio")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if p.LocationRaw != "" {
		t.Errorf("LocationRaw = %q, want empty rather than an invented place", p.LocationRaw)
	}
}

func TestParse_RawIsValidJSON(t *testing.T) {
	got, err := New(nil, "test").Parse(fixture(t, "board-full.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, p := range got {
		if !json.Valid(p.Raw) {
			t.Errorf("%s: Raw is not valid JSON", p.ExternalID)
		}
	}
}

func TestParse_SkipsJobsWithNoID(t *testing.T) {
	got, err := New(nil, "test").Parse(
		[]byte(`{"meta":{"totalCount":2},"result":[{"jobOpeningName":"No id"},{"id":"9","jobOpeningName":"Fine"}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Fine" {
		t.Errorf("got %+v", got)
	}
}

func TestParse_RejectsMalformed(t *testing.T) {
	if _, err := New(nil, "test").Parse([]byte(`{"result":`)); err == nil {
		t.Fatal("want an error for truncated JSON, got nil")
	}
}

var _ = source.VendorBambooHR
