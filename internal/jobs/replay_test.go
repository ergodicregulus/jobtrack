package jobs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/config"
	"github.com/ergodicregulus/jobtrack/internal/source"
)

// These are the first tests in this package. It is one of the three named in
// CLAUDE.md's gap table, and the thing being proved here is the reason to start
// with it: INGEST_MODE was a setting nothing read, so the package that decides
// whether requests leave the process had no test saying they do not.

func deps(t *testing.T, mode string) *Deps {
	t.Helper()
	return &Deps{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg: &config.Config{Ingest: config.Ingest{
			Mode:      mode,
			UserAgent: "JobTrackBot/1.0 (test)",
		}},
	}
}

// The invariant, stated as a type: only `live` gets a client that can dial.
//
// Asserting on the concrete type rather than on observed traffic is deliberate.
// A behavioural test can only prove that no request happened on the paths it
// exercised; this proves no path could, because there is nothing to dial with.
func TestHTTPClient_OnlyLiveModeCanReachTheNetwork(t *testing.T) {
	for _, mode := range []string{"fixture", "recorded", ""} {
		if _, ok := deps(t, mode).httpClient().(*source.Replay); !ok {
			t.Errorf("mode %q got a real HTTP client; INGEST_MODE only permits the network in live mode", mode)
		}
	}
	if _, ok := deps(t, "live").httpClient().(*http.Client); !ok {
		t.Error("live mode must get a real client, or nothing can ever be ingested")
	}
}

// Every adapter parses the board its replay serves, through its own Fetch — not
// through Parse on a fixture, which the golden tests already cover. This is the
// pagination, conditional-request and detail-sweep code, which is where the
// mapping in replay.go can be wrong in ways a unit test cannot see.
//
// A vendor with zero postings here means a seeded corpus is silently empty, which
// is precisely how CI's e2e suite came to fail on `li.card` not existing.
func TestFetch_EveryAdapterYieldsPostingsFromItsReplay(t *testing.T) {
	d := deps(t, "fixture")
	d.Init()

	// Board tokens in the shape each adapter expects. Workday parses its token
	// into datacentre/tenant/site, so it cannot be an arbitrary string.
	tokens := map[source.Vendor]string{
		source.VendorGreenhouse:      "stripe",
		source.VendorAshby:           "ramp",
		source.VendorSmartRecruiters: "BoschGroup",
		source.VendorRecruitee:       "acme",
		source.VendorWorkable:        "acme",
		source.VendorWorkday:         "wd1/acme/careers",
		source.VendorPersonio:        "acme",
		source.VendorKeka:            "spyneai:jobs/org-123",
		source.VendorBambooHR:        "posthog",
	}

	for vendor, token := range tokens {
		t.Run(string(vendor), func(t *testing.T) {
			adapter, ok := d.adapterFor(vendor)
			if !ok {
				t.Fatalf("no adapter registered for %s", vendor)
			}
			result, err := adapter.Fetch(context.Background(), source.Source{
				ID: 1, CompanyID: 1, Vendor: vendor, BoardToken: token,
			})
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if len(result.Postings) == 0 {
				t.Fatalf("no postings; a board that replays to nothing seeds an empty corpus")
			}
			// One field that must survive every adapter, or the posting cannot be
			// deduplicated or linked.
			for i, p := range result.Postings {
				if p.ExternalID == "" {
					t.Errorf("posting %d has no ExternalID", i)
				}
				if p.Title == "" {
					t.Errorf("posting %d (%s) has no Title", i, p.ExternalID)
				}
			}
		})
	}
}

// The second poll of an unchanged board must be cheap. Replay sets an ETag and
// honours If-None-Match, so this exercises the branch the real ingestor takes
// almost every time it runs — and proves the fixtures are stable byte-for-byte.
func TestFetch_SecondPollOfAnUnchangedBoardIsNotModified(t *testing.T) {
	d := deps(t, "fixture")
	d.Init()

	adapter, ok := d.adapterFor(source.VendorGreenhouse)
	if !ok {
		t.Fatal("no greenhouse adapter")
	}
	src := source.Source{ID: 1, CompanyID: 1, Vendor: source.VendorGreenhouse, BoardToken: "stripe"}

	first, err := adapter.Fetch(context.Background(), src)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if first.NotModified {
		t.Fatal("first fetch reported NotModified with no validator sent")
	}

	// Feed back what the store would have persisted.
	src.ETag, src.ContentHash = first.ETag, first.ContentHash
	second, err := adapter.Fetch(context.Background(), src)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if !second.NotModified {
		t.Error("an unchanged board was fetched and parsed again; the conditional path is not working")
	}
}
