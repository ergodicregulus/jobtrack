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

// INGEST_LIVE_ALLOWLIST was checked only for being non-empty, so naming one source
// fetched all of them. These are the semantics it now has.
func TestAllowlist_LiveModeFetchesOnlyWhatItNames(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		entries []string
		permit  map[int64]bool
	}{
		{"replay limits nothing, it reaches no network", "fixture", nil,
			map[int64]bool{1: true, 99: true}},
		{"live names two sources", "live", []string{"3", "7"},
			map[int64]bool{3: true, 7: true, 1: false, 99: false}},
		{"live * is every registered source", "live", []string{"*"},
			map[int64]bool{1: true, 99: true}},
		{"live with nothing named permits nothing", "live", nil,
			map[int64]bool{1: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newAllowlist(tc.mode, tc.entries)
			for id, want := range tc.permit {
				if got := a.permits(id); got != want {
					t.Errorf("permits(%d) = %v, want %v", id, got, want)
				}
			}
		})
	}
}

// River shares the ingestor's pool. Writers must leave it room, or it cannot
// fetch or complete jobs; and a pool too small to leave room still writes.
func TestWriteSlots_LeaveRiverItsConnections(t *testing.T) {
	for _, tc := range []struct {
		maxConns int32
		want     int
	}{{5, 2}, {10, 7}, {3, 1}, {1, 1}} {
		if got := writeSlotsFor(tc.maxConns); got != tc.want {
			t.Errorf("writeSlotsFor(%d) = %d, want %d", tc.maxConns, got, tc.want)
		}
	}
}
