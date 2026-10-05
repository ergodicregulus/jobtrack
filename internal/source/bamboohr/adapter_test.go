package bamboohr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// locationType is the arrangement BambooHR fills; isRemote is null on flyio.
func TestConvert_LocationTypeIsTheWorkplace(t *testing.T) {
	a := New(nil, "test")
	var lr listResponse
	if err := json.Unmarshal(fixture(t, "board-full.json"), &lr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p, err := a.convert(&lr.Result[0], "flyio")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if p.WorkplaceType != "remote" {
		t.Errorf("locationType %q gave WorkplaceType %q, want remote", lr.Result[0].LocationType, p.WorkplaceType)
	}
	for in, want := range map[string]string{"0": "onsite", "1": "remote", "2": "hybrid", "": "", "9": ""} {
		if got := workplace(&wireJob{LocationType: in}); got != want {
			t.Errorf("workplace(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeBambooHR serves a list of n postings and a body for every detail request.
type fakeBambooHR struct{ n int }

func (f fakeBambooHR) Do(req *http.Request) (*http.Response, error) {
	body := `{"result":{"jobOpening":{"description":"<p>body</p>","datePosted":"2026-09-01"}}}`
	if strings.HasSuffix(req.URL.Path, "/careers/list") {
		var b strings.Builder
		fmt.Fprintf(&b, `{"meta":{"totalCount":%d},"result":[`, f.n)
		for i := 0; i < f.n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"id":"%d","jobOpeningName":"Role %d","locationType":"1"}`, i, i)
		}
		b.WriteString(`]}`)
		body = b.String()
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// The detail bound must never shorten the board: a posting missing from the
// result is closed as absent.
func TestFetch_ABoardPastTheDetailBoundIsReturnedWhole(t *testing.T) {
	n := detailsPerPoll + 15
	res, err := New(fakeBambooHR{n: n}, "test").Fetch(context.Background(), source.Source{BoardToken: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Postings) != n {
		t.Fatalf("returned %d of %d postings; the rest would be closed as absent", len(res.Postings), n)
	}
	if res.DetailRequested != detailsPerPoll || res.DetailFilled != detailsPerPoll {
		t.Errorf("detail requested %d, filled %d; want %d each",
			res.DetailRequested, res.DetailFilled, detailsPerPoll)
	}
}
