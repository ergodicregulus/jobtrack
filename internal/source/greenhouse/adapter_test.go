package greenhouse

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jobtrack/jobtrack/internal/source"
)

// Golden-file testing is what makes a seven-vendor ingestion layer maintainable.
// Every quirk documented in docs/research/source-catalog.md#greenhouse has a
// fixture here, so a vendor schema change fails exactly one test and changes
// exactly one file.
//
// Regenerate with:  go test ./internal/source/greenhouse -update
// Then READ THE DIFF. A blind -update that accepts a regression is the one way
// this technique fails.

var update = flag.Bool("update", false, "rewrite golden files")

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// assertGolden compares normalised output against a committed snapshot.
func assertGolden(t *testing.T, name string, got any) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)

	pretty, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	pretty = append(pretty, '\n')

	if *update {
		if err := os.WriteFile(path, pretty, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated %s — review the diff before committing", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if string(pretty) != string(want) {
		t.Errorf("output does not match %s\n--- got ---\n%s\n--- want ---\n%s",
			path, pretty, want)
	}
}

func newAdapter() *Adapter { return New(nil, "test-agent") }

func TestParse_FullBoard(t *testing.T) {
	got, err := newAdapter().Parse(loadFixture(t, "board-full.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("parsed %d postings, want 3", len(got))
	}

	// Raw is the vendor payload, retained for reprocessing but not part of the
	// normalisation contract — excluding it keeps the golden file readable and
	// stops it churning on unrelated field-order changes.
	for i := range got {
		got[i].Raw = nil
	}
	assertGolden(t, "board-full.json", got)
}

// The single most important Greenhouse quirk: `content` is HTML that is itself
// HTML-escaped, and the official example is DOUBLE escaped. Getting this wrong
// means every description renders as literal `&lt;p&gt;` text.
func TestParse_DecodesEscapedHTML(t *testing.T) {
	t.Run("single escaped", func(t *testing.T) {
		got, err := newAdapter().Parse(loadFixture(t, "board-full.json"))
		if err != nil {
			t.Fatal(err)
		}
		desc := got[0].DescriptionHTML
		if !strings.Contains(desc, "<p>") || !strings.Contains(desc, "<ul>") {
			t.Errorf("description was not unescaped to real HTML:\n%s", desc)
		}
		if strings.Contains(desc, "&lt;") {
			t.Errorf("description still contains escaped entities:\n%s", desc)
		}
	})

	t.Run("double escaped", func(t *testing.T) {
		got, err := newAdapter().Parse(loadFixture(t, "board-double-escaped.json"))
		if err != nil {
			t.Fatal(err)
		}
		desc := got[0].DescriptionHTML
		if !strings.Contains(desc, "<p>") {
			t.Errorf("double-escaped content was not fully decoded:\n%s", desc)
		}
		if strings.Contains(desc, "&lt;") || strings.Contains(desc, "&amp;") {
			t.Errorf("double-escaped content still contains entities:\n%s", desc)
		}
	})
}

// decodeContent must be idempotent on already-clean HTML, or a future double
// call would start eating real markup.
func TestDecodeContent_IdempotentOnCleanHTML(t *testing.T) {
	clean := "<p>Already clean &amp; fine</p>"
	once := decodeContent(clean)
	twice := decodeContent(once)
	if once != twice {
		t.Errorf("decodeContent is not idempotent:\n once: %q\ntwice: %q", once, twice)
	}
}

// An empty board is legitimate (a company with no openings) and must parse
// cleanly. Treating it as an error would disable healthy sources.
func TestParse_EmptyBoardIsValid(t *testing.T) {
	got, err := newAdapter().Parse(loadFixture(t, "board-empty.json"))
	if err != nil {
		t.Fatalf("an empty board should parse cleanly, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d postings from an empty board", len(got))
	}
}

// A response claiming 214 jobs but carrying none is truncated, not a mass
// closure. Closing 214 postings on a bad payload is the most damaging thing
// this pipeline could do, so it must be an error.
func TestParse_TruncatedResponseIsRejected(t *testing.T) {
	_, err := newAdapter().Parse(loadFixture(t, "board-truncated.json"))
	if !errors.Is(err, source.ErrSuspiciousEmpty) {
		t.Fatalf("meta.total=214 with zero jobs must be ErrSuspiciousEmpty, got: %v", err)
	}
}

// One malformed row must not discard the rest of the board.
func TestParse_SkipsBadRowsKeepsGoodOnes(t *testing.T) {
	got, err := newAdapter().Parse(loadFixture(t, "board-malformed.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d postings, want 1 (the valid row)", len(got))
	}
	if got[0].Title != "Valid Role" {
		t.Errorf("kept the wrong row: %q", got[0].Title)
	}
}

func TestParse_RejectsInvalidJSON(t *testing.T) {
	_, err := newAdapter().Parse([]byte(`{"jobs": [`))
	if !errors.Is(err, source.ErrMalformed) {
		t.Fatalf("invalid JSON should be ErrMalformed, got: %v", err)
	}
}

// requisition_id is a free dedup key: two postings from one company sharing it
// are the same role, decided without any similarity computation.
func TestParse_ExtractsRequisitionID(t *testing.T) {
	got, err := newAdapter().Parse(loadFixture(t, "board-full.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].RequisitionID != "ENG-482" {
		t.Errorf("RequisitionID = %q, want ENG-482", got[0].RequisitionID)
	}
	// An absent requisition_id must be empty, not the string "null".
	if got[2].RequisitionID != "" {
		t.Errorf("missing requisition_id should be empty, got %q", got[2].RequisitionID)
	}
}

// The list endpoint has no first_published, so age comes from updated_at and
// MUST be flagged as an estimate. updated_at moves on any edit, so treating it
// as a publication date makes an edited 40-day-old posting look fresh —
// corrupting the exact signal the product is built on.
func TestParse_MarksListDatesAsEstimates(t *testing.T) {
	got, err := newAdapter().Parse(loadFixture(t, "board-full.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.PostedAt == nil {
			t.Errorf("%s: PostedAt is nil", p.Title)
			continue
		}
		if !p.PostedAtIsEstimate {
			t.Errorf("%s: list-endpoint date must be marked as an estimate", p.Title)
		}
	}
}

// The detail endpoint has first_published, which is authoritative.
func TestParseDetail_UsesFirstPublished(t *testing.T) {
	got, err := newAdapter().ParseDetail(loadFixture(t, "detail-pay-ranges.json"))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	if got.PostedAtIsEstimate {
		t.Error("first_published is authoritative and must not be marked an estimate")
	}
	want := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if !got.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v, want %v (first_published, not updated_at)", got.PostedAt, want)
	}
}

// Greenhouse publishes per-location bands. We take the widest envelope: showing
// a narrower range than the employer offers would understate the role, and we
// cannot know which band applies to a given candidate.
func TestParseDetail_PayRangesTakeWidestEnvelope(t *testing.T) {
	got, err := newAdapter().ParseDetail(loadFixture(t, "detail-pay-ranges.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.CompMin == nil || got.CompMax == nil {
		t.Fatal("structured compensation was not extracted")
	}
	// min of (2_800_000, 3_000_000) and max of (4_200_000, 4_500_000), in units.
	if *got.CompMin != 2_800_000 {
		t.Errorf("CompMin = %v, want 2800000", *got.CompMin)
	}
	if *got.CompMax != 4_500_000 {
		t.Errorf("CompMax = %v, want 4500000", *got.CompMax)
	}
	if got.CompCurrency != "INR" {
		t.Errorf("CompCurrency = %q, want INR", got.CompCurrency)
	}
	if !got.CompIsStructured {
		t.Error("CompIsStructured must be true for pay_input_ranges")
	}
}

// NULL compensation means "not disclosed" and must stay distinguishable from
// zero all the way to the UI — collapsing them loses ~20% of the market.
func TestParse_UndisclosedCompStaysNil(t *testing.T) {
	got, err := newAdapter().Parse(loadFixture(t, "board-full.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.CompMin != nil {
			t.Errorf("%s: list endpoint has no structured comp, but CompMin is set", p.Title)
		}
		if p.CompIsStructured {
			t.Errorf("%s: CompIsStructured must be false without pay_input_ranges", p.Title)
		}
	}
}

// Greenhouse exposes whether the employer runs AI talent matching, and an
// opt-out URL. Absent means UNKNOWN, never "no AI" — other vendors do not
// expose it at all.
func TestParseDetail_AIScreeningDisclosure(t *testing.T) {
	t.Run("disclosed true with opt-out", func(t *testing.T) {
		got, err := newAdapter().ParseDetail(loadFixture(t, "detail-ai-disclaimer.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got.AIScreeningDisclosed == nil || !*got.AIScreeningDisclosed {
			t.Fatal("include_ai_disclaimer=true was not captured")
		}
		if got.AIOptOutURL == "" {
			t.Error("ai_opt_out_request_url was not captured — it is the actionable part")
		}
	})

	t.Run("disclosed false", func(t *testing.T) {
		got, err := newAdapter().ParseDetail(loadFixture(t, "detail-no-ai.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got.AIScreeningDisclosed == nil || *got.AIScreeningDisclosed {
			t.Error("include_ai_disclaimer=false should be captured as an explicit false")
		}
	})

	t.Run("absent means unknown", func(t *testing.T) {
		got, err := newAdapter().Parse(loadFixture(t, "board-full.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got[0].AIScreeningDisclosed != nil {
			t.Error("the list endpoint omits the field; it must stay nil (unknown), not false")
		}
	})
}

func TestParseTime(t *testing.T) {
	cases := map[string]bool{
		"2026-08-12T09:14:22-04:00": true,
		"2026-08-14T11:02:00Z":      true,
		"2026-08-14T11:02:00":       true,
		"2026-08-14":                true,
		"not a date":                false,
		"":                          false,
	}
	for in, wantOK := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := parseTime(in)
			if (err == nil) != wantOK {
				t.Errorf("parseTime(%q): err = %v, wantOK = %v", in, err, wantOK)
			}
		})
	}
}

// --- Fetch behaviour, via a stub transport (never a live ATS) ---------------

type stubClient struct {
	resp *http.Response
	err  error
	req  *http.Request
}

func (s *stubClient) Do(req *http.Request) (*http.Response, error) {
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func response(status int, body string, headers map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     h,
	}
}

// Conditional requests are what make 2-hourly polling of watched companies
// affordable. If validators stop being sent, cost rises ~10x and vendors start
// rate-limiting us.
func TestFetch_SendsConditionalHeaders(t *testing.T) {
	stub := &stubClient{resp: response(http.StatusNotModified, "", nil)}
	a := New(stub, "JobTrackBot/1.0")

	_, err := a.Fetch(context.Background(), source.Source{
		BoardToken:   "acmepay",
		ETag:         `W/"abc123"`,
		LastModified: "Wed, 12 Aug 2026 09:14:22 GMT",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if got := stub.req.Header.Get("If-None-Match"); got != `W/"abc123"` {
		t.Errorf("If-None-Match = %q, want the stored ETag", got)
	}
	if got := stub.req.Header.Get("If-Modified-Since"); got == "" {
		t.Error("If-Modified-Since was not sent")
	}
	if got := stub.req.Header.Get("User-Agent"); got != "JobTrackBot/1.0" {
		t.Errorf("User-Agent = %q; an identifying agent is how vendors reach us "+
			"instead of blocking us", got)
	}
}

func TestFetch_NotModifiedCarriesValidatorsForward(t *testing.T) {
	// A 304 need not repeat the validators, so we must keep the ones we sent —
	// otherwise the next poll is unconditional and the saving is lost.
	stub := &stubClient{resp: response(http.StatusNotModified, "", nil)}
	a := New(stub, "test")

	got, err := a.Fetch(context.Background(), source.Source{
		BoardToken: "x", ETag: `"keep-me"`, LastModified: "Mon, 01 Jan 2026 00:00:00 GMT",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.NotModified {
		t.Fatal("304 should set NotModified")
	}
	if got.ETag != `"keep-me"` {
		t.Errorf("ETag = %q, want the previous value carried forward", got.ETag)
	}
}

// The content hash is the second line of change detection, for vendors that
// always return 200. It skips parsing and every downstream job.
func TestFetch_UnchangedContentHashSkipsParsing(t *testing.T) {
	body := `{"jobs":[],"meta":{"total":0}}`
	first := &stubClient{resp: response(http.StatusOK, body, nil)}
	a := New(first, "test")

	r1, err := a.Fetch(context.Background(), source.Source{BoardToken: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if r1.NotModified {
		t.Fatal("first fetch cannot be NotModified")
	}

	second := &stubClient{resp: response(http.StatusOK, body, nil)}
	a2 := New(second, "test")
	r2, err := a2.Fetch(context.Background(), source.Source{
		BoardToken: "x", ContentHash: r1.ContentHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r2.NotModified {
		t.Error("an identical body must be detected via content hash")
	}
}

func TestFetch_ErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		wantErr error
	}{
		{"404 is a dead board", http.StatusNotFound, nil, source.ErrSourceGone},
		{"410 is a dead board", http.StatusGone, nil, source.ErrSourceGone},
		{"429 is rate limiting", http.StatusTooManyRequests,
			map[string]string{"Retry-After": "60"}, source.ErrRateLimited},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New(&stubClient{resp: response(tc.status, "", tc.headers)}, "test")
			got, err := a.Fetch(context.Background(), source.Source{BoardToken: "x"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.status == http.StatusTooManyRequests && got.RetryAfter != time.Minute {
				t.Errorf("RetryAfter = %v, want 1m — it must be honoured exactly", got.RetryAfter)
			}
		})
	}
}

func TestVendor(t *testing.T) {
	if got := newAdapter().Vendor(); got != source.VendorGreenhouse {
		t.Errorf("Vendor() = %q, want greenhouse", got)
	}
}
