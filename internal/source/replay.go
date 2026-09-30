package source

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Replay serves adapter requests from the committed golden fixtures instead of
// the network. It is what INGEST_MODE=fixture means.
//
// The mode was documented from the first commit — ".env.example: replay golden
// files. No network at all" — and implemented by nothing: the config loader
// validated the value, cmd/ingestor logged it, and internal/jobs called
// adapter.Fetch unconditionally. So a development machine polled live ATS
// endpoints while its own configuration said it did not, and the
// INGEST_LIVE_ALLOWLIST guard gated the label rather than any request.
//
// It sits at the HTTPDoer boundary rather than inside each adapter, because
// that is the one place every vendor already passes through. No adapter changes,
// and the whole Fetch path still runs — pagination, conditional requests, the
// detail sweep, the cursor arithmetic. Those are the parts that break; a replay
// that skipped them would test the parser and nothing else.
//
// What it deliberately does not do is pretend to be per-company truth. Every
// board of a vendor replays that vendor's captured board, so a seeded corpus has
// a realistic shape, volume and field distribution, with the same postings under
// many companies. That is the trade that buys determinism and no network.
// ADR-0021.
type Replay struct {
	// root is the module root. Fixtures are read from
	// <root>/internal/source/<vendor>/testdata, which is where the golden tests
	// already keep them — copying them somewhere embeddable would create a
	// second copy to drift.
	root string
}

// NewReplay returns a Replay reading fixtures under root.
//
// An empty root means "find the module root", by walking up from the working
// directory to the directory holding go.mod. Resolving against the working
// directory alone was the first attempt and the first test killed it: `go test
// ./internal/jobs/` runs two directories down, so every fixture was missing. The
// containers happen to run from /src, which would have made this look correct
// everywhere except where someone actually ran it.
//
// A binary with no module root nearby — a production image — gets an error naming
// the setting on its first request, which is right: fixture mode has no business
// in production, and failing loudly beats ingesting nothing quietly.
func NewReplay(root string) *Replay {
	if root == "" {
		root = findModuleRoot()
	}
	return &Replay{root: root}
}

// findModuleRoot walks up from the working directory to the go.mod directory.
// It returns "" when there is none, which surfaces as a fixture-not-found error
// carrying the path it tried.
func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// fixture names the file that answers one request.
type fixture struct {
	vendor Vendor
	file   string
}

// contentType is derived from the extension. Personio is the only XML feed, and
// an adapter that received JSON headers for XML would fail in a way that looks
// like a parser bug.
func (f fixture) contentType() string {
	if strings.HasSuffix(f.file, ".xml") {
		return "application/xml"
	}
	return "application/json"
}

// Do resolves the request to a fixture and returns it as a synthetic response.
//
// It honours If-None-Match so the conditional-request path is exercised rather
// than bypassed: a second poll of an unchanged board takes the 304 branch, which
// is how the real ingestor spends most of its life.
func (r *Replay) Do(req *http.Request) (*http.Response, error) {
	f, err := resolveFixture(req.URL)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(r.root, "internal", "source", string(f.vendor), "testdata", f.file)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay %s: %w (INGEST_MODE=fixture needs the repository's testdata; set INGEST_MODE=live to fetch)", f.vendor, err)
	}

	// Derived from the bytes, so it is stable across restarts and changes when a
	// fixture is re-captured — which is exactly what an ETag should do.
	etag := fmt.Sprintf(`"replay-%s-%s-%d"`, f.vendor, strings.TrimSuffix(f.file, filepath.Ext(f.file)), len(body))

	header := http.Header{}
	header.Set("Content-Type", f.contentType())
	header.Set("ETag", etag)

	if match := req.Header.Get("If-None-Match"); match != "" && match == etag {
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Header:     header,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}

	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}

// resolveFixture maps a request to the fixture that answers it.
//
// Host patterns rather than a URL table, because the adapters build URLs from a
// board token and there are 98 boards across 9 vendors. The patterns are the
// ones documented in docs/research/source-catalog.md and they are the vendors'
// public API hostnames, which do not move.
//
// An unrecognised host is an ERROR, never a passthrough. A replay that quietly
// fell back to the network would be worse than no replay: the guarantee this
// type exists to provide is "no request leaves the process".
func resolveFixture(u *url.URL) (fixture, error) {
	host, path := u.Hostname(), u.Path

	switch {
	case host == "boards-api.greenhouse.io":
		// /v1/boards/{token}/jobs is the board; a further segment is one job.
		if after, ok := cutAfter(path, "/jobs/"); ok && after != "" {
			return fixture{VendorGreenhouse, "detail-pay-ranges.json"}, nil
		}
		return fixture{VendorGreenhouse, "board-full.json"}, nil

	case host == "api.ashbyhq.com":
		return fixture{VendorAshby, "board-full.json"}, nil

	case host == "api.smartrecruiters.com":
		if after, ok := cutAfter(path, "/postings/"); ok && after != "" {
			return fixture{VendorSmartRecruiters, "detail.json"}, nil
		}
		return fixture{VendorSmartRecruiters, "list.json"}, nil

	case strings.HasSuffix(host, ".recruitee.com"):
		return fixture{VendorRecruitee, "board-full.json"}, nil

	case host == "apply.workable.com":
		return fixture{VendorWorkable, "board-full.json"}, nil

	case strings.Contains(host, ".myworkdayjobs.com"):
		// The list is a POST to .../jobs; everything else under /wday/cxs is a
		// job or its related payload.
		if strings.HasSuffix(path, "/jobs") {
			return fixture{VendorWorkday, "board-full.json"}, nil
		}
		return fixture{VendorWorkday, "detail.json"}, nil

	case strings.HasSuffix(host, ".jobs.personio.de"):
		return fixture{VendorPersonio, "board-full.xml"}, nil

	case strings.HasSuffix(host, ".keka.com"):
		return fixture{VendorKeka, "board-full.json"}, nil

	case strings.HasSuffix(host, ".bamboohr.com"):
		if strings.HasSuffix(path, "/detail") {
			return fixture{VendorBambooHR, "detail-35.json"}, nil
		}
		return fixture{VendorBambooHR, "board-full.json"}, nil
	}

	return fixture{}, fmt.Errorf(
		"replay: no fixture for host %q (INGEST_MODE=fixture makes no network requests; "+
			"add a mapping in internal/source/replay.go or set INGEST_MODE=live)", host)
}

// cutAfter returns what follows sep, and whether sep was present.
func cutAfter(s, sep string) (string, bool) {
	_, after, ok := strings.Cut(s, sep)
	return after, ok
}
