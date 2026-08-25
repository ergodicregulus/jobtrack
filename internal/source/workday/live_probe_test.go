//go:build liveprobe

package workday

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jobtrack/jobtrack/internal/source"
)

// A live probe, behind a build tag so it never runs in CI.
//
// The golden-file rule exists because a suite that depends on someone else's
// uptime fails for reasons that have nothing to do with the change under test.
// This is the deliberate exception: proof that the facet walk actually gets
// beneath the cap against a real board, run by hand.
//
//	go test -tags=liveprobe -run TestLiveNvidia -v ./internal/source/workday/
func TestLiveNvidia(t *testing.T) {
	a := New(&http.Client{Timeout: 90 * time.Second},
		"JobTrackBot/1.0 (+https://jobtrack.dev/bot)")

	res, err := a.Fetch(context.Background(), source.Source{
		BoardToken: "wd5/nvidia/nvidiaexternalcareersite",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	withDesc, withDate := 0, 0
	for _, p := range res.Postings {
		if p.DescriptionHTML != "" {
			withDesc++
		}
		if p.PostedAt != nil {
			withDate++
		}
	}
	t.Logf("postings=%d withDescription=%d withRealDate=%d cursor=%d",
		len(res.Postings), withDesc, withDate, res.DetailCursor)

	// The whole point. `total` reports 2,000; the facets say ~2,630.
	if len(res.Postings) <= 2000 {
		t.Errorf("got %d postings — the facet walk did not get beneath the cap",
			len(res.Postings))
	}
	if withDesc == 0 {
		t.Error("no descriptions filled; the detail phase did nothing")
	}
}
