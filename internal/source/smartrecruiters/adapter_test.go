package smartrecruiters

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jobtrack/jobtrack/internal/source"
)

// Golden fixtures are captured real responses from BoschGroup, the largest
// public SmartRecruiters board. Never a live call in a test: an adapter suite
// that depends on someone else's API is a suite that goes red when they deploy.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func TestParse_MapsAListPage(t *testing.T) {
	a := New(nil, "test")
	got, err := a.Parse(fixture(t, "list.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no postings parsed")
	}

	p := got[0]
	if p.ExternalID == "" {
		t.Error("ExternalID is empty — dedup and upsert both key on it")
	}
	if p.Title == "" {
		t.Error("Title is empty")
	}
	if p.LocationRaw == "" {
		t.Error("LocationRaw is empty")
	}
	// releasedDate is the vendor's FIRST-published timestamp, which is unusual
	// and valuable: freshness is real here rather than an upper bound.
	if p.PostedAt == nil {
		t.Error("PostedAt is nil; releasedDate should have parsed")
	}
	if p.PostedAtIsEstimate {
		t.Error("PostedAtIsEstimate is true, but releasedDate is a real first-publication date")
	}
	// The list carries no bodies. Asserting this pins the two-phase design: if
	// the vendor ever inlines descriptions, this test fails and someone reads
	// why the second phase exists.
	if p.DescriptionHTML != "" {
		t.Error("list page produced a description; the second fetch may now be unnecessary")
	}
}

func TestParseDetail_KeepsTheQualificationsHeading(t *testing.T) {
	a := New(nil, "test")
	p, err := a.ParseDetail(fixture(t, "detail.json"))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}

	if p.DescriptionHTML == "" {
		t.Fatal("no description")
	}

	// The entire reason this vendor is worth adapting. The skill extractor
	// classifies a skill by the heading above it, and "Qualifications" is a
	// requirements heading — so these postings yield real must-haves where a
	// single undifferentiated blob yields only `mentioned`.
	if !strings.Contains(p.DescriptionHTML, "<h2>Qualifications</h2>") {
		t.Errorf("the Qualifications heading was lost; without it these postings "+
			"score no better than any other source. Got:\n%.400s", p.DescriptionHTML)
	}

	// Ordering matters as much as presence. companyDescription is boilerplate
	// naming technologies used across the whole group; above the qualifications
	// it would let a company-wide name-drop read as a requirement for this role.
	jd := strings.Index(p.DescriptionHTML, "<h2>Job Description</h2>")
	qual := strings.Index(p.DescriptionHTML, "<h2>Qualifications</h2>")
	company := strings.Index(p.DescriptionHTML, "<h2>Company Description</h2>")
	if jd < 0 || qual < 0 {
		t.Fatal("expected both a job description and qualifications section")
	}
	if jd > qual {
		t.Error("Job Description must precede Qualifications")
	}
	if company >= 0 && company < qual {
		t.Error("Company Description must come LAST, or its technology name-drops " +
			"are read as requirements for this specific role")
	}
}

func TestConvert_RequisitionIDIsCarried(t *testing.T) {
	a := New(nil, "test")
	got, err := a.Parse(fixture(t, "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	// refNumber is the employer's own requisition code, which is a free dedup
	// key — two postings sharing it are the same role, with no similarity
	// computation at all.
	var found bool
	for _, p := range got {
		if p.RequisitionID != "" {
			found = true
		}
	}
	if !found {
		t.Error("no posting carried a RequisitionID; the free dedup key was dropped")
	}
}

func TestParse_MalformedBodyIsAnErrorNotAPanic(t *testing.T) {
	a := New(nil, "test")
	if _, err := a.Parse([]byte("{not json")); err == nil {
		t.Error("malformed JSON parsed successfully")
	}
	// An empty board is valid, not an error: a company can genuinely have no
	// open roles, and treating that as a failure would alarm on a normal state.
	got, err := a.Parse([]byte(`{"offset":0,"limit":100,"totalFound":0,"content":[]}`))
	if err != nil {
		t.Errorf("empty board: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty board produced %d postings", len(got))
	}
}

// A posting with no id cannot be upserted or deduped, so it is dropped rather
// than stored under an empty key where it would collide with the next one.
func TestParse_PostingWithoutIDIsDropped(t *testing.T) {
	a := New(nil, "test")
	got, err := a.Parse([]byte(`{"content":[
	  {"id":"","name":"Ghost"},
	  {"id":"1","name":"Real","releasedDate":"2026-08-01T00:00:00.000Z"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Real" {
		t.Errorf("expected only the posting with an id, got %+v", got)
	}
}

// --- The second phase, and whether it actually finishes ---

// fakeBoard serves a SmartRecruiters board of n postings: paginated list pages
// and one detail document per posting. It records every detail ID it is asked
// for, which is the thing under test — the list phase is already covered above.
type fakeBoard struct {
	n int

	// failDetails makes every detail request 500, standing in for a board whose
	// bodies we cannot read.
	failDetails bool

	// detailConcurrency workers call Do at once, so the record of what they
	// asked for needs a lock. -race finds this immediately if it is missing.
	mu        sync.Mutex
	requested []string
}

func (f *fakeBoard) record(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requested = append(f.requested, id)
}

func (f *fakeBoard) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requested...)
}

func (f *fakeBoard) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requested = nil
}

func (f *fakeBoard) Do(req *http.Request) (*http.Response, error) {
	reply := func(body string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}

	// Detail requests end in the posting ID; list requests carry a query.
	if req.URL.RawQuery == "" {
		id := path.Base(req.URL.Path)
		f.record(id)
		if f.failDetails {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Request:    req,
			}, nil
		}
		return reply(`{"id":"` + id + `","name":"Role ` + id + `","jobAd":{"sections":{}}}`)
	}

	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	var b strings.Builder
	fmt.Fprintf(&b, `{"offset":%d,"limit":%d,"totalFound":%d,"content":[`, offset, limit, f.n)
	for i := offset; i < offset+limit && i < f.n; i++ {
		if i > offset {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"%d","name":"Role %d","releasedDate":"2026-08-01T00:00:00.000Z",`+
			`"location":{"city":"Berlin","country":"de"}}`, i, i)
	}
	b.WriteString(`]}`)
	return reply(b.String())
}

func fetchBoard(t *testing.T, board *fakeBoard, src source.Source) source.FetchResult {
	t.Helper()
	src.BoardToken = "TestBoard"
	res, err := New(board, "test").Fetch(context.Background(), src)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return res
}

// A board larger than maxDetailFetches must be filled in over successive polls.
// Before this test the window was always [0, 250) — every poll re-fetched the
// same first 250 bodies and the rest of the board never got one, which left
// 1,861 of 2,393 live SmartRecruiters postings with no description at all.
func TestFetch_DetailSweepAdvancesAcrossPolls(t *testing.T) {
	board := &fakeBoard{n: 3 * maxDetailFetches}

	first := fetchBoard(t, board, source.Source{})
	if got := len(board.seen()); got != maxDetailFetches {
		t.Fatalf("first poll fetched %d details, want %d", got, maxDetailFetches)
	}
	if first.DetailCursor != maxDetailFetches {
		t.Fatalf("cursor after first poll = %d, want %d", first.DetailCursor, maxDetailFetches)
	}
	firstPass := board.seen()

	board.reset()
	second := fetchBoard(t, board, source.Source{
		DetailCursor: first.DetailCursor,
		ContentHash:  first.ContentHash,
	})
	if second.NotModified {
		t.Fatal("an unfinished sweep was skipped as NotModified; the rest of the board would never get a body")
	}
	if second.DetailCursor != 2*maxDetailFetches {
		t.Fatalf("cursor after second poll = %d, want %d", second.DetailCursor, 2*maxDetailFetches)
	}

	seen := make(map[string]bool, len(firstPass))
	for _, id := range firstPass {
		seen[id] = true
	}
	for _, id := range board.seen() {
		if seen[id] {
			t.Fatalf("second poll re-fetched %s; the window did not advance", id)
		}
	}
}

// Once the whole board has been swept, an unchanged board costs one request.
// This is the saving the two-phase design exists for and must survive the fix.
func TestFetch_UnchangedBoardIsSkippedOnlyAfterAFullSweep(t *testing.T) {
	board := &fakeBoard{n: 10}

	first := fetchBoard(t, board, source.Source{})
	if first.DetailCursor != board.n {
		t.Fatalf("cursor = %d, want %d — a board smaller than the budget sweeps in one poll",
			first.DetailCursor, board.n)
	}

	board.reset()
	second := fetchBoard(t, board, source.Source{
		DetailCursor: first.DetailCursor,
		ContentHash:  first.ContentHash,
	})
	if !second.NotModified {
		t.Error("an unchanged board with a finished sweep was re-fetched")
	}
	if n := len(board.seen()); n != 0 {
		t.Errorf("fetched %d details for an unchanged, fully-swept board", n)
	}
}

// maxPages must cover the largest board we actually watch, or postings beyond
// the cap are invisible and ReconcileAbsent treats them as gone.
func TestMaxPages_CoversTheLargestBoardWeWatch(t *testing.T) {
	// BoschGroup, measured 2026-08-19: totalFound 4,770 and growing.
	const largestBoardWatched = 4770
	if maxPages*pageSize < largestBoardWatched {
		t.Fatalf("maxPages*pageSize = %d, which truncates a %d-posting board",
			maxPages*pageSize, largestBoardWatched)
	}
}

// A sweep that was cut off must not be recorded as a sweep that happened.
// Two live boards advanced 250 places having filled nothing, because the
// database pool was saturated and every detail fetch was cancelled — those
// postings would then have waited for the next full pass to get a body.
func TestFetch_AnInterruptedSweepDoesNotAdvanceTheCursor(t *testing.T) {
	board := &fakeBoard{n: 3 * maxDetailFetches, failDetails: true}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := New(board, "test").Fetch(ctx, source.Source{BoardToken: "TestBoard"})
	if err != nil {
		// A cancelled list phase is a plain error and never reaches the cursor.
		return
	}
	if res.DetailCursor != 0 {
		t.Errorf("cursor advanced to %d after a sweep that filled nothing under a "+
			"cancelled context; those postings would be skipped", res.DetailCursor)
	}
}

// A window whose postings genuinely have no body must still advance, or one
// permanently-missing document stalls the sweep for the whole board.
func TestFetch_AFailedBodyStillAdvancesTheSweep(t *testing.T) {
	board := &fakeBoard{n: 3 * maxDetailFetches, failDetails: true}

	res := fetchBoard(t, board, source.Source{})
	if res.DetailCursor != maxDetailFetches {
		t.Errorf("cursor = %d, want %d — unfetchable bodies must not stall the board",
			res.DetailCursor, maxDetailFetches)
	}
}

// The list response carries no applyUrl, so a posting whose body has not been
// fetched yet used to reach the feed with nothing to click — 5,050 live
// postings, a third of the corpus, were in that state. The apply link is the
// product's entire output; it cannot wait for a second request.
func TestParse_EveryPostingHasAnApplyLinkFromTheListAlone(t *testing.T) {
	a := New(nil, "test")
	got, err := a.Parse(fixture(t, "list.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no postings parsed")
	}
	for _, p := range got {
		if p.ApplyURL == "" {
			t.Fatalf("%q has no apply link", p.Title)
		}
		if !strings.HasPrefix(p.ApplyURL, "https://") {
			t.Fatalf("%q apply link is not absolute: %q", p.Title, p.ApplyURL)
		}
		if !strings.Contains(p.ApplyURL, p.ExternalID) {
			t.Fatalf("%q apply link %q does not address the posting", p.Title, p.ApplyURL)
		}
	}
}

// The detail document's own URLs are richer (they carry the title slug and the
// apply-tracking parameter) and must win over the derived one.
func TestParseDetail_PrefersTheVendorsOwnURL(t *testing.T) {
	p, err := New(nil, "test").ParseDetail(fixture(t, "detail.json"))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	if p.ApplyURL == "" {
		t.Fatal("detail produced no apply link")
	}
	if p.ApplyURL == publicURL("BoschGroup", p.ExternalID) {
		t.Error("fell back to the derived URL while the detail document supplied one")
	}
}

func TestPublicURL_RefusesToBuildABrokenLink(t *testing.T) {
	if got := publicURL("", "123"); got != "" {
		t.Errorf("publicURL with no board token = %q, want empty", got)
	}
	if got := publicURL("BoschGroup", ""); got != "" {
		t.Errorf("publicURL with no id = %q, want empty", got)
	}
}
