package source

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The whole point of the type: every vendor resolves to a fixture that exists on
// disk. A mapping that compiles and points at a missing file is the failure this
// catches, and it is the one that would otherwise surface as an empty corpus
// three jobs later in CI.
func TestReplay_EveryVendorResolvesToAFixtureThatExists(t *testing.T) {
	r := NewReplay("")

	// One representative URL per vendor, in the shape its adapter builds.
	cases := map[Vendor]string{
		VendorGreenhouse:      "https://boards-api.greenhouse.io/v1/boards/stripe/jobs?content=true",
		VendorAshby:           "https://api.ashbyhq.com/posting-api/job-board/ramp?includeCompensation=true",
		VendorSmartRecruiters: "https://api.smartrecruiters.com/v1/companies/BoschGroup/postings?limit=100&offset=0",
		VendorRecruitee:       "https://acme.recruitee.com/api/offers/",
		VendorWorkable:        "https://apply.workable.com/api/v1/widget/accounts/acme?details=true",
		VendorWorkday:         "https://acme.wd1.myworkdayjobs.com/wday/cxs/acme/careers/jobs",
		VendorPersonio:        "https://acme.jobs.personio.de/xml",
		VendorKeka:            "https://spyneai.keka.com/careers/api/embedjobs/portal/active/org",
		VendorBambooHR:        "https://posthog.bamboohr.com/careers/list",
	}

	for vendor, raw := range cases {
		t.Run(string(vendor), func(t *testing.T) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			f, err := resolveFixture(u)
			if err != nil {
				t.Fatalf("no fixture: %v", err)
			}
			if f.vendor != vendor {
				t.Errorf("resolved to vendor %q, want %q", f.vendor, vendor)
			}

			req, err := http.NewRequest(http.MethodGet, raw, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := r.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
			if resp.Header.Get("ETag") == "" {
				t.Error("no ETag; the conditional-request path would never be exercised")
			}
			if resp.ContentLength <= 0 {
				t.Errorf("ContentLength = %d; an empty board parses to nothing",
					resp.ContentLength)
			}
		})
	}
}

// Detail endpoints must not resolve to the board fixture. Returning a list where
// a single job was requested is the kind of mismatch that surfaces as a parser
// error in an unrelated package.
func TestReplay_DetailEndpointsResolveToDetailFixtures(t *testing.T) {
	cases := map[string]string{
		"https://boards-api.greenhouse.io/v1/boards/stripe/jobs/4567":            "detail-pay-ranges.json",
		"https://api.smartrecruiters.com/v1/companies/BoschGroup/postings/abc99": "detail.json",
		"https://posthog.bamboohr.com/careers/35/detail":                         "detail-35.json",
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		f, err := resolveFixture(u)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if f.file != want {
			t.Errorf("%s resolved to %q, want %q", raw, f.file, want)
		}
	}
}

// An unmapped host must be an error, not a passthrough. If this ever returns a
// response by reaching the network, the guarantee the type exists to provide is
// gone and INGEST_MODE=fixture is a lie again.
func TestReplay_UnknownHostIsAnErrorRatherThanARequest(t *testing.T) {
	r := NewReplay("")
	req, err := http.NewRequest(http.MethodGet, "https://jobs.example.com/api/v1/postings", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := r.Do(req)
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("an unmapped host returned a response; fixture mode must refuse, never fall back to the network")
	}
	if got := err.Error(); !strings.Contains(got, "jobs.example.com") {
		t.Errorf("error should name the host so it can be mapped, got: %s", got)
	}
}

// A repeated poll of an unchanged board takes the 304 branch, which is where the
// real ingestor spends most of its life. A replay that always answered 200 would
// leave that path untested by every adapter at once.
func TestReplay_HonoursIfNoneMatch(t *testing.T) {
	r := NewReplay("")
	const raw = "https://boards-api.greenhouse.io/v1/boards/stripe/jobs?content=true"

	first, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := r.Do(first)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	etag := resp.Header.Get("ETag")

	second, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	second.Header.Set("If-None-Match", etag)
	again, err := r.Do(second)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304 for a matching ETag", again.StatusCode)
	}
}

// A missing fixture must say what is missing and what to do. The previous
// behaviour of this whole subsystem was to silently do something other than what
// the configuration said, and a bare os.ErrNotExist would be a quieter version of
// the same problem.
func TestReplay_MissingFixtureRootExplainsItself(t *testing.T) {
	r := NewReplay(filepath.Join(os.TempDir(), "jobtrack-replay-not-a-repo"))
	req, err := http.NewRequest(http.MethodGet,
		"https://boards-api.greenhouse.io/v1/boards/stripe/jobs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Do(req); err == nil {
		t.Fatal("a missing fixture root was accepted")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error should wrap os.ErrNotExist so callers can tell it apart, got: %v", err)
	} else if !strings.Contains(err.Error(), "INGEST_MODE") {
		t.Errorf("error should name the setting responsible, got: %v", err)
	}
}
