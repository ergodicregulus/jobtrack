package workday

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

func TestParseBoard(t *testing.T) {
	a := New(nil, "test")
	got, err := a.Parse(fixture(t, "board-full.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no postings parsed from a full board")
	}

	for i, p := range got {
		if p.ExternalID == "" {
			t.Errorf("posting %d has no external id", i)
		}
		if p.Title == "" {
			t.Errorf("posting %d has no title", i)
		}
		// The list carries no trustworthy date. Setting one from "Posted
		// Today" would manufacture precision the source does not have, so the
		// detail phase is the only thing allowed to fill this in.
		if p.PostedAt != nil {
			t.Errorf("posting %d has a PostedAt from the LIST response; only "+
				"the detail endpoint carries a real date", i)
		}
	}
}

// TestTruncationIsDetected is the test phase-5 requires of any capped vendor,
// and the reason this adapter exists in the shape it does.
//
// Workday pins `total` at 2,000. The fixture is a real shape: total says 2,000
// while the facet counts sum to 2,630. An adapter that believes `total` walks
// away 630 postings short and reports success — which is exactly how BoschGroup
// was truncated to 41% of its board for a week.
func TestTruncationIsDetected(t *testing.T) {
	capped := fixture(t, "board-truncated.json")

	if !IsTruncated(capped) {
		t.Error("a response with total=2000 and facets summing to 2,630 must " +
			"be reported as truncated")
	}

	var list wireList
	if err := jsonUnmarshal(capped, &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	actual, ok := trueTotal(&list)
	if !ok {
		t.Fatal("facets should yield a true total")
	}
	if actual != 2630 {
		t.Errorf("true total = %d, want 2630 (the largest facet group's sum)", actual)
	}
	if actual <= 2000 {
		t.Error("the whole point is that the true total EXCEEDS the reported one")
	}
}

func TestEmptyBoardIsNotTruncated(t *testing.T) {
	empty := fixture(t, "board-empty.json")

	got, err := New(nil, "test").Parse(empty)
	if err != nil {
		t.Fatalf("Parse(empty): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty board parsed %d postings", len(got))
	}
	// A board with nothing on it is not a board we failed to read. Conflating
	// them would disable healthy sources.
	if IsTruncated(empty) {
		t.Error("an empty board must not be reported as truncated")
	}
}

func TestMalformedBoardIsAnError(t *testing.T) {
	if _, err := New(nil, "test").Parse(fixture(t, "board-malformed.json")); err == nil {
		t.Error("truncated JSON must be an error, not an empty board — the " +
			"second would silently close every posting the source has")
	}
}

// TestDetailSuppliesWhatTheListCannot covers the fields that only exist on the
// second call, and the date in particular: the list says "Posted Today" and the
// detail says 2026-08-25.
func TestDetailSuppliesWhatTheListCannot(t *testing.T) {
	var d wireDetail
	if err := jsonUnmarshal(fixture(t, "detail.json"), &d); err != nil {
		t.Fatalf("decode detail: %v", err)
	}

	p := struct{ have bool }{}
	_ = p

	var raw = fixture(t, "board-full.json")
	posts, err := New(nil, "test").Parse(raw)
	if err != nil || len(posts) == 0 {
		t.Fatalf("need a posting to enrich: %v", err)
	}

	target := posts[0]
	applyDetail(&target, &d)

	if target.DescriptionHTML == "" {
		t.Error("detail must supply the description; the list has none")
	}
	if target.ApplyURL == "" {
		t.Error("detail must supply the apply URL")
	}
	if target.PostedAt == nil {
		t.Fatal("detail must supply a real posted date from startDate")
	}
	if got := target.PostedAt.Format("2006-01-02"); got != "2026-08-25" {
		t.Errorf("PostedAt = %s, want 2026-08-25 (startDate, not \"Posted Today\")", got)
	}
	if target.RequisitionID == "" {
		t.Error("detail must supply the requisition id")
	}
}

// TestSliceFacetPrefersVerifiableFacets guards the choice of walking axis: a
// facet whose values carry no count cannot be checked for truncation, so it is
// worse than a coarser one that can.
func TestSliceFacetPrefersVerifiableFacets(t *testing.T) {
	var list wireList
	if err := jsonUnmarshal(fixture(t, "board-full.json"), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}

	name, vals, ok := sliceFacet(&list)
	if !ok {
		t.Fatal("a real board should offer a facet to slice by")
	}
	if len(vals) < 2 {
		t.Errorf("facet %q has %d values; slicing by it buys nothing", name, len(vals))
	}
	for _, v := range vals {
		if v.Count == 0 {
			t.Errorf("facet %q has an uncountable value %q — truncation cannot "+
				"be verified per slice", name, v.Descriptor)
		}
	}
}

// jsonUnmarshal keeps the tests readable where they need the wire types.
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
