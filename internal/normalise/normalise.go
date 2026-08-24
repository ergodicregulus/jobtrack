// Package normalise turns vendor-shaped postings into the canonical form.
//
// This is where almost all of ingestion's real engineering cost lives. Fetching
// is easy; agreeing on what "remote", "3+ years" and "₹28-42 LPA" mean across
// seven vendors is not.
//
// One rule runs through everything here: **our uncertainty never becomes the
// user's penalty.** Every extraction carries a confidence, and a low-confidence
// value is stored as unknown rather than as a guess. A posting we failed to
// parse must never be filtered away from someone it actually suits.
package normalise

import (
	"regexp"
	"strings"
)

// Confidence is a 0..1 score for one extraction.
type Confidence float64

const (
	// ConfidenceCertain is for structured vendor data — a numeric field, not a
	// guess from prose.
	ConfidenceCertain Confidence = 1.0
	// ConfidenceStrong is a well-formed textual pattern, e.g. "3-5 years".
	ConfidenceStrong Confidence = 0.9
	// ConfidenceWeak is an inferred value, e.g. seniority implying a YoE band.
	ConfidenceWeak Confidence = 0.5
	// ConfidenceNone means we could not extract anything.
	ConfidenceNone Confidence = 0.0

	// MinUsableConfidence is the threshold below which a value is treated as
	// unknown by filters rather than used. Set so an inferred band never
	// silently excludes a posting from someone's results.
	MinUsableConfidence Confidence = 0.5
)

// stripTags removes HTML so text extraction sees prose, not markup.
//
// Not a sanitiser — sanitisation for rendering happens separately and to an
// allowlist. This exists only so a regex does not match inside an attribute.
var tagPattern = regexp.MustCompile(`<[^>]*>`)

// StripHTML converts markup to plain text for analysis.
func StripHTML(s string) string {
	if s == "" {
		return ""
	}
	// Block-level tags become newlines so "5+ years</li><li>Go" does not become
	// "5+ yearsGo" and defeat every subsequent pattern.
	s = blockTagPattern.ReplaceAllString(s, "\n")
	s = tagPattern.ReplaceAllString(s, " ")
	s = htmlEntity.Replace(s)
	return collapseSpace(s)
}

var blockTagPattern = regexp.MustCompile(`(?i)</(p|div|li|ul|ol|h[1-6]|br|tr)>|<br\s*/?>`)

var htmlEntity = strings.NewReplacer(
	"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
	"&quot;", `"`, "&#39;", "'", "&rsquo;", "'", "&ndash;", "-", "&mdash;", "-",
)

var multiSpace = regexp.MustCompile(`[ \t]+`)
var multiNewline = regexp.MustCompile(`\n{3,}`)

// spaceAroundNewline exists because replacing `</li>` with a newline leaves the
// following `<li>` to become a space: "years\n Go". Downstream section-heading
// matching is anchored at line start, so a leading space breaks it.
var spaceAroundNewline = regexp.MustCompile(`[ \t]*\n[ \t]*`)

func collapseSpace(s string) string {
	s = multiSpace.ReplaceAllString(s, " ")
	s = spaceAroundNewline.ReplaceAllString(s, "\n")
	s = multiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// --- Title ------------------------------------------------------------------

// seniorityTokens are stripped from the normalised title so that
// "Senior Backend Engineer" and "Backend Engineer" block together during dedup.
// The seniority itself is preserved separately — it feeds the YoE inference.
var seniorityTokens = []string{
	"senior", "sr", "staff", "principal", "lead", "head of", "director",
	"junior", "jr", "entry level", "entry-level", "associate", "graduate",
	"intern", "trainee", "apprentice",
}

var levelSuffix = regexp.MustCompile(`(?i)\b(i{1,3}|iv|v|[1-5])\b\s*$`)
var nonAlphaNum = regexp.MustCompile(`[^a-z0-9\s]+`)

// Title returns the normalised title and the detected seniority.
//
// Normalisation is aggressive on purpose: it is only used for dedup blocking
// and matching. The original title is what the user sees, always.
func Title(raw string) (normalised, seniority string) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", ""
	}

	// Expand abbreviations before stripping punctuation, or "Sr." loses its dot
	// and stops matching.
	s = titleAbbrev.Replace(s)

	for _, tok := range seniorityTokens {
		if containsWord(s, tok) {
			seniority = tok
			s = removeWord(s, tok)
		}
	}

	s = nonAlphaNum.ReplaceAllString(s, " ")
	s = levelSuffix.ReplaceAllString(s, "")
	return collapseSpace(s), seniority
}

var titleAbbrev = strings.NewReplacer(
	"sr.", "senior", "sr ", "senior ",
	"jr.", "junior", "jr ", "junior ",
	"sde", "software engineer",
	"swe", "software engineer",
	"eng.", "engineer",
	"mgr", "manager",
)

func containsWord(s, word string) bool {
	idx := strings.Index(s, word)
	if idx < 0 {
		return false
	}
	before := idx == 0 || !isWordChar(s[idx-1])
	end := idx + len(word)
	after := end >= len(s) || !isWordChar(s[end])
	return before && after
}

func removeWord(s, word string) string {
	return strings.ReplaceAll(s, word, " ")
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// --- Years of experience ----------------------------------------------------

// Ordered most-specific first: "3-5 years" must win over "3+ years", which must
// win over a bare "5 years".
var yoePatterns = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`(?i)\b(\d{1,2})\s*[-–—to]+\s*(\d{1,2})\b\s*\+?\s*(?:years?|yrs?)`), "range"},
	{regexp.MustCompile(`(?i)\b(\d{1,2})\s*\+\s*(?:years?|yrs?)`), "min"},
	{regexp.MustCompile(`(?i)(?:at least|minimum(?: of)?|min\.?)\s*\b(\d{1,2})\b\s*(?:years?|yrs?)`), "min"},
	{regexp.MustCompile(`(?i)\b(\d{1,2})\b\s*(?:years?|yrs?)\s+(?:of\s+)?(?:relevant\s+|professional\s+|industry\s+)?experience`), "min"},
}

// seniorityYoE is the fallback when the text states no number. Deliberately
// low-confidence: it is an inference from a job title, not a stated
// requirement, and filters treat it as unknown.
var seniorityYoE = map[string][2]int16{
	"intern": {0, 1}, "trainee": {0, 1}, "apprentice": {0, 1},
	"graduate": {0, 2}, "entry level": {0, 2}, "entry-level": {0, 2},
	"junior": {0, 2}, "jr": {0, 2},
	"associate": {1, 3},
	"senior":    {5, 10}, "sr": {5, 10},
	"staff": {8, 15}, "principal": {10, 20}, "lead": {7, 12},
}

// YoE extracts a years-of-experience band from the description.
//
// Returns nils when nothing usable is found. That matters: a nil band is
// treated as "unknown" and the posting stays visible to everyone, whereas
// guessing would hide it from exactly the people it might suit.
func YoE(text, seniority string) (min, max *int16, conf Confidence) {
	for _, p := range yoePatterns {
		m := p.re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		switch p.kind {
		case "range":
			lo, hi := atoi16(m[1]), atoi16(m[2])
			if lo > hi || hi > 40 {
				continue // "20-3 years" or "100 years" is a false positive
			}
			return &lo, &hi, ConfidenceStrong
		case "min":
			lo := atoi16(m[1])
			// A stated minimum of 0 carries no information, and >30 is a
			// misparse rather than a real requirement.
			if lo <= 0 || lo > 30 {
				continue
			}
			return &lo, nil, ConfidenceStrong
		}
	}

	if band, ok := seniorityYoE[seniority]; ok {
		lo, hi := band[0], band[1]
		return &lo, &hi, ConfidenceWeak
	}
	return nil, nil, ConfidenceNone
}

func atoi16(s string) int16 {
	var n int16
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int16(c-'0')
	}
	return n
}

// --- Work mode --------------------------------------------------------------

// WorkMode mirrors the SQL enum.
type WorkMode string

const (
	ModeOnsite  WorkMode = "onsite"
	ModeHybrid  WorkMode = "hybrid"
	ModeRemote  WorkMode = "remote"
	ModeUnknown WorkMode = "unknown"
)

var (
	hybridPattern = regexp.MustCompile(`(?i)\bhybrid\b|\b\d\s*days?\s+(?:a\s+week\s+)?(?:in|at)\s+(?:the\s+)?office\b|\bin[- ]office\s+\d\s*days?`)
	remotePattern = regexp.MustCompile(`(?i)\b(?:fully\s+)?remote\b|\bwork\s+from\s+home\b|\bdistributed\s+team\b|\bwfh\b`)
	onsitePattern = regexp.MustCompile(`(?i)\bon[- ]?site\b|\bin[- ]person\b|\bno\s+remote\b`)
)

// Mode derives the working arrangement.
//
// The vendor's own flag is a hint, never truth. A posting tagged "remote" whose
// body says "3 days in the office" is hybrid, and the description wins —
// mislabelled remote roles are one of the most common complaints about every
// aggregator.
func Mode(vendorFlag, locationRaw, text string) WorkMode {
	haystack := strings.ToLower(locationRaw + "\n" + text)

	// Hybrid is checked first because hybrid postings almost always also say
	// "remote" somewhere, and the more specific claim should win.
	if hybridPattern.MatchString(haystack) {
		return ModeHybrid
	}

	// An explicit on-site claim is checked before any remote match, because
	// "no remote" and "on-site only" both CONTAIN the word remote. Checking
	// remote first would invert the meaning of the most explicit statement in
	// the posting.
	onsite := onsitePattern.MatchString(haystack)
	vendor := normaliseVendorMode(vendorFlag)

	switch {
	case onsite && vendor == ModeRemote:
		// The vendor and the prose disagree. Hybrid is the honest middle: we
		// know it is not fully remote and not fully on-site.
		return ModeHybrid
	case onsite:
		return ModeOnsite
	case vendor == ModeRemote:
		return ModeRemote
	case remotePattern.MatchString(haystack):
		return ModeRemote
	case vendor != ModeUnknown:
		return vendor
	default:
		return ModeUnknown
	}
}

func normaliseVendorMode(v string) WorkMode {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "remote", "fully_remote", "telecommute":
		return ModeRemote
	case "hybrid":
		return ModeHybrid
	case "onsite", "on_site", "on-site", "in_office":
		return ModeOnsite
	default:
		return ModeUnknown
	}
}
