package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func filterFor(t *testing.T, query string) (bands []string, err error) {
	t.Helper()
	r := httptest.NewRequest("GET", "/v1/jobs?"+query, nil)
	f, err := parseFeedFilter(r)
	return f.Bands, err
}

// The band filter exists so a dashboard tile can land the user on the set it
// counted. If the two disagree the tile is worse than useless, so the parsing
// is pinned here.
func TestParseFeedFilter_Bands(t *testing.T) {
	t.Run("single band", func(t *testing.T) {
		bands, err := filterFor(t, "band=strong")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bands) != 1 || bands[0] != "strong" {
			t.Errorf("got %v, want [strong]", bands)
		}
	})

	t.Run("comma separated", func(t *testing.T) {
		bands, err := filterFor(t, "band=strong,plausible")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bands) != 2 {
			t.Errorf("got %v, want two bands", bands)
		}
	})

	t.Run("case and spacing are forgiven", func(t *testing.T) {
		bands, err := filterFor(t, "band=Strong,%20PLAUSIBLE")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bands) != 2 || bands[0] != "strong" || bands[1] != "plausible" {
			t.Errorf("got %v, want [strong plausible]", bands)
		}
	})

	// A typo must not return an empty feed. "Nothing matches you" and "you
	// mistyped a filter" are completely different statements, and only one of
	// them is recoverable by the user.
	t.Run("an unknown band is rejected, not silently dropped", func(t *testing.T) {
		_, err := filterFor(t, "band=strongg")
		if err == nil {
			t.Fatal("expected an error for an unknown band")
		}
		if !strings.Contains(err.Error(), "strongg") {
			t.Errorf("error should name the offending value, got %q", err)
		}
	})

	t.Run("absent means unfiltered", func(t *testing.T) {
		bands, err := filterFor(t, "mode=remote")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bands) != 0 {
			t.Errorf("got %v, want no band filter", bands)
		}
	})

	// Mirrors the check constraint on user_job_scores.band. A value accepted
	// here but rejected by the database would surface as a 500 rather than a
	// 400, which is the wrong error for bad input.
	t.Run("every stored band is accepted", func(t *testing.T) {
		for _, b := range []string{"strong", "plausible", "stretch", "unlikely"} {
			if _, err := filterFor(t, "band="+b); err != nil {
				t.Errorf("band %q was rejected but exists in the schema: %v", b, err)
			}
		}
	})
}

// An unknown parameter is a 400 rather than being ignored: a typo'd filter that
// quietly returns unfiltered results is the worst failure here, because the user
// believes they are looking at a filtered list.
func TestParseFeedFilter_UnknownParamRejected(t *testing.T) {
	if _, err := filterFor(t, "bands=strong"); err == nil {
		t.Error("expected 'bands' (plural) to be rejected as unknown")
	}
}
