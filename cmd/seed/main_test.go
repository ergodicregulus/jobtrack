package main

import "testing"

// Seeding rewrites posted_at so a replayed corpus is fresh enough to render. It
// must never do that to live postings, whose dates are observations: that would
// fabricate the one number on a card that was actually read from the source.
func TestSpreadsDemoDates_NeverRewritesLiveDates(t *testing.T) {
	if spreadsDemoDates("live") {
		t.Fatal("live postings would have their real posted_at overwritten")
	}
	for _, mode := range []string{"fixture", "recorded"} {
		if !spreadsDemoDates(mode) {
			t.Errorf("mode %q replays fixtures and needs its dates spread, or the feed is empty", mode)
		}
	}
}
