package normalise

import (
	"strings"
	"testing"
)

// Normalisation is where ingestion is most likely to be quietly wrong: a bad
// parse does not error, it just produces a posting that is filtered away from
// the person it suits. Every test here is a specific way that happens.

func TestStripHTML_BlockTagsBecomeNewlines(t *testing.T) {
	// Without this, "5+ years</li><li>Go" becomes "5+ yearsGo" and every
	// downstream pattern — YoE, skills, section headings — stops matching.
	got := StripHTML("<ul><li>5+ years</li><li>Go</li></ul>")
	if got != "5+ years\n\nGo" && got != "5+ years\nGo" {
		t.Errorf("block tags did not become line breaks: %q", got)
	}
}

func TestStripHTML_DecodesEntities(t *testing.T) {
	got := StripHTML("<p>R&amp;D team &ndash; 5&nbsp;years</p>")
	for _, unwanted := range []string{"&amp;", "&ndash;", "&nbsp;"} {
		if contains(got, unwanted) {
			t.Errorf("entity %q survived: %q", unwanted, got)
		}
	}
}

func TestTitle(t *testing.T) {
	cases := []struct {
		raw           string
		wantNorm      string
		wantSeniority string
	}{
		// Seniority is stripped so "Senior Backend Engineer" and "Backend
		// Engineer" block together during dedup.
		{"Senior Backend Engineer", "backend engineer", "senior"},
		{"Sr. Software Engineer", "software engineer", "senior"},
		{"Staff Engineer, Infrastructure", "engineer infrastructure", "staff"},
		{"Junior Developer", "developer", "junior"},
		{"SDE-1", "software engineer", ""},
		{"Software Engineer II", "software engineer", ""},
		{"Backend Engineer", "backend engineer", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			gotNorm, gotSen := Title(tc.raw)
			if gotNorm != tc.wantNorm {
				t.Errorf("normalised = %q, want %q", gotNorm, tc.wantNorm)
			}
			if gotSen != tc.wantSeniority {
				t.Errorf("seniority = %q, want %q", gotSen, tc.wantSeniority)
			}
		})
	}
}

func TestYoE(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		seniority string
		wantMin   *int16
		wantMax   *int16
		wantConf  Confidence
	}{
		{"explicit range", "We need 3-5 years of experience", "", i16(3), i16(5), ConfidenceStrong},
		{"en dash range", "3–5 years", "", i16(3), i16(5), ConfidenceStrong},
		{"plus form", "5+ years building distributed systems", "", i16(5), nil, ConfidenceStrong},
		{"at least", "at least 2 years of professional experience", "", i16(2), nil, ConfidenceStrong},
		{"bare years of experience", "8 years of relevant experience", "", i16(8), nil, ConfidenceStrong},

		// A range wins over a bare minimum appearing later in the text.
		{"range beats min", "3-5 years required. 10+ years preferred.", "", i16(3), i16(5), ConfidenceStrong},

		// Seniority inference is weak on purpose — filters treat it as unknown,
		// so a senior-titled role is never hidden from a 3-year engineer.
		{"seniority fallback", "No numbers here at all", "senior", i16(5), i16(10), ConfidenceWeak},
		{"junior fallback", "No numbers here", "junior", i16(0), i16(2), ConfidenceWeak},

		// Nothing usable: nil band means "unknown", and the posting stays
		// visible to everyone.
		{"nothing", "A lovely role with great people", "", nil, nil, ConfidenceNone},

		// False positives that must be rejected.
		{"implausible range", "20-3 years", "", nil, nil, ConfidenceNone},
		{"absurd number", "100 years of experience", "", nil, nil, ConfidenceNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMin, gotMax, gotConf := YoE(tc.text, tc.seniority)
			assertI16(t, "min", gotMin, tc.wantMin)
			assertI16(t, "max", gotMax, tc.wantMax)
			if gotConf != tc.wantConf {
				t.Errorf("confidence = %v, want %v", gotConf, tc.wantConf)
			}
		})
	}
}

// The vendor's flag is a hint, never truth. A posting tagged "remote" whose body
// says "3 days in the office" is hybrid — mislabelled remote roles are the most
// common complaint about every aggregator.
func TestMode_DescriptionOverridesVendorFlag(t *testing.T) {
	cases := []struct {
		name       string
		vendorFlag string
		location   string
		text       string
		want       WorkMode
	}{
		{"vendor says remote, body says 3 days in office", "remote", "Bengaluru",
			"This is a remote-friendly role. Expect 3 days in the office.", ModeHybrid},
		{"vendor says remote, nothing contradicts", "remote", "Remote",
			"Fully distributed team.", ModeRemote},
		{"explicit hybrid in location", "", "Hybrid — Pune, India", "", ModeHybrid},
		{"remote in text only", "", "India", "This is a fully remote position.", ModeRemote},
		{"onsite in text", "", "Bengaluru", "This role is on-site, no remote.", ModeOnsite},
		{"vendor onsite, no signal in text", "onsite", "Bengaluru", "Great team.", ModeOnsite},
		{"no signal anywhere", "", "Bengaluru", "Great team.", ModeUnknown},
		{"wfh abbreviation", "", "", "WFH available", ModeRemote},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Mode(tc.vendorFlag, tc.location, tc.text); got != tc.want {
				t.Errorf("Mode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseLocation(t *testing.T) {
	cases := []struct {
		raw         string
		wantCity    string
		wantCountry string
		wantRegion  string
	}{
		{"Bengaluru, India", "Bengaluru", "IN", "KA"},
		// Alias resolution: the same city under three names must agree.
		{"Bangalore", "Bengaluru", "IN", "KA"},
		{"BLR", "Bengaluru", "IN", "KA"},
		{"Mumbai, India", "Mumbai", "IN", "MH"},
		{"Gurgaon", "Gurugram", "IN", "HR"},
		{"San Francisco, CA, US", "San Francisco", "US", ""},
		{"London, United Kingdom", "London", "GB", ""},

		// Multi-location resolves to the first concrete place; the raw string is
		// always preserved and shown, so nothing is hidden from the user.
		{"Bangalore / Hyderabad / Remote", "Bengaluru", "IN", "KA"},
		{"Remote - India", "", "IN", ""},

		// Nothing concrete: country stays empty, which filters read as unknown
		// rather than as a mismatch.
		{"Remote", "", "", ""},
		{"", "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseLocation(tc.raw)
			if got.City != tc.wantCity {
				t.Errorf("City = %q, want %q", got.City, tc.wantCity)
			}
			if got.Country != tc.wantCountry {
				t.Errorf("Country = %q, want %q", got.Country, tc.wantCountry)
			}
			if tc.wantRegion != "" && got.Region != tc.wantRegion {
				t.Errorf("Region = %q, want %q", got.Region, tc.wantRegion)
			}
			if got.Raw != tc.raw {
				t.Errorf("Raw must be preserved verbatim: got %q", got.Raw)
			}
		})
	}
}

func TestParseCompensation(t *testing.T) {
	cases := []struct {
		name         string
		text         string
		wantMin      float64
		wantMax      float64
		wantCurrency string
		wantParsed   bool
	}{
		{"INR LPA range", "Compensation: ₹28-42 LPA", 2_800_000, 4_200_000, "INR", true},
		{"INR LPA with words", "The salary range is 25 - 40 lakhs per annum", 2_500_000, 4_000_000, "INR", true},
		{"USD k-notation", "Salary: $180k – $220k", 180_000, 220_000, "USD", true},
		{"USD full numbers", "Base pay of $180,000 - $220,000", 180_000, 220_000, "USD", true},
		{"GBP k-notation", "Compensation £90k-£120k", 90_000, 120_000, "GBP", true},

		// The failure modes that matter. Each of these is a real way naive
		// salary parsers produce nonsense.
		{"version number is not a salary", "Requires Java 17 and Spring Boot 3", 0, 0, "", false},
		{"team size is not a salary", "Join a team of 8-12 engineers", 0, 0, "", false},
		{"years is not a salary", "5-7 years of experience required", 0, 0, "", false},
		{"no compensation mentioned", "A great role with a great team", 0, 0, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCompensation(tc.text)

			if !tc.wantParsed {
				if got.Disclosed() {
					t.Fatalf("expected no compensation, got min=%v max=%v %s",
						deref(got.Min), deref(got.Max), got.Currency)
				}
				return
			}

			if !got.Disclosed() {
				t.Fatal("expected compensation to be parsed, got none")
			}
			if *got.Min != tc.wantMin {
				t.Errorf("Min = %v, want %v", *got.Min, tc.wantMin)
			}
			if tc.wantMax > 0 {
				if got.Max == nil {
					t.Fatalf("Max is nil, want %v", tc.wantMax)
				}
				if *got.Max != tc.wantMax {
					t.Errorf("Max = %v, want %v", *got.Max, tc.wantMax)
				}
			}
			if got.Currency != tc.wantCurrency {
				t.Errorf("Currency = %q, want %q", got.Currency, tc.wantCurrency)
			}
			// A parsed value must be marked as such, so the UI can distinguish
			// a published range from one we read out of prose.
			if got.Source != "parsed_text" {
				t.Errorf("Source = %q, want parsed_text", got.Source)
			}
		})
	}
}

// Undisclosed must stay distinguishable from zero, all the way to the UI.
func TestCompensation_UndisclosedIsNotZero(t *testing.T) {
	c := ParseCompensation("No salary information here")
	if c.Disclosed() {
		t.Fatal("expected undisclosed")
	}
	if c.Min != nil || c.Max != nil {
		t.Error("undisclosed compensation must have nil bounds, not zero values")
	}
}

// The must-have / nice-to-have split is the single largest driver of score
// quality. Treating everything mentioned as required makes every score
// uniformly low, and a uniformly low score cannot rank anything.
func TestExtractSkills_SectionAwareClassification(t *testing.T) {
	v := DefaultVocabulary()
	desc := StripHTML(`
		<p>About the role</p>
		<p>We use Kafka internally.</p>
		<h3>Requirements</h3>
		<ul><li>Strong Go</li><li>PostgreSQL at scale</li></ul>
		<h3>Nice to have</h3>
		<ul><li>Kubernetes</li><li>Terraform</li></ul>
	`)

	got := v.ExtractSkills(desc)
	byName := make(map[string]ExtractedSkill, len(got))
	for _, s := range got {
		byName[s.Canonical] = s
	}

	requireReq := func(skill string, want Requirement) {
		t.Helper()
		s, ok := byName[skill]
		if !ok {
			t.Errorf("%s was not extracted", skill)
			return
		}
		if s.Requirement != want {
			t.Errorf("%s classified as %s, want %s", skill, s.Requirement, want)
		}
	}

	requireReq("go", MustHave)
	requireReq("postgresql", MustHave)
	requireReq("kubernetes", NiceToHave)
	requireReq("terraform", NiceToHave)
	// Named before any requirements heading: a passing reference, not a
	// requirement.
	requireReq("kafka", Mentioned)
}

// Aliases must collapse to one canonical skill, or "Postgres" and "PostgreSQL"
// become two different things and matching silently halves.
func TestExtractSkills_AliasesCollapse(t *testing.T) {
	v := DefaultVocabulary()
	for _, text := range []string{
		"Requirements\nExperience with Postgres",
		"Requirements\nExperience with PostgreSQL",
		"Requirements\nExperience with psql",
	} {
		got := v.ExtractSkills(text)
		found := false
		for _, s := range got {
			if s.Canonical == "postgresql" {
				found = true
			}
		}
		if !found {
			t.Errorf("did not resolve to canonical postgresql: %q", text)
		}
	}
}

// Punctuated skills are exactly the ones people care about, and exactly the
// ones a naive \b word boundary fails on: Go's \b does not treat '+' or '#'
// as word characters.
func TestExtractSkills_PunctuatedNames(t *testing.T) {
	v := DefaultVocabulary()
	for _, tc := range []struct{ text, want string }{
		{"Requirements\nStrong C++ background", "c++"},
		{"Requirements\nC# and .NET experience", "c#"},
		{"Requirements\nNode.js services", "node.js"},
	} {
		got := v.ExtractSkills(tc.text)
		found := false
		for _, s := range got {
			if s.Canonical == tc.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not extract %q; got %v", tc.text, tc.want, names(got))
		}
	}
}

// The classic false positive: "go" inside "going", "ongoing", "algorithm".
func TestExtractSkills_NoSubstringFalsePositives(t *testing.T) {
	v := DefaultVocabulary()
	got := v.ExtractSkills("Requirements\nOngoing algorithm work, going to scale, a good rust-proof plan")
	for _, s := range got {
		if s.Canonical == "go" {
			t.Error(`matched "go" inside a longer word — word boundaries are broken`)
		}
	}
}

// A skill in both sections is a must-have: the stronger classification wins.
func TestExtractSkills_StrongestClassificationWins(t *testing.T) {
	v := DefaultVocabulary()
	got := v.ExtractSkills("Nice to have\nKafka\nRequirements\nKafka and Go")
	for _, s := range got {
		if s.Canonical == "kafka" && s.Requirement != MustHave {
			t.Errorf("kafka appears in both sections; must_have should win, got %s", s.Requirement)
		}
	}
}

// --- helpers ---

func i16(v int16) *int16 { return &v }

func assertI16(t *testing.T, label string, got, want *int16) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil:
		t.Errorf("%s = nil, want %d", label, *want)
	case want == nil:
		t.Errorf("%s = %d, want nil", label, *got)
	case *got != *want:
		t.Errorf("%s = %d, want %d", label, *got, *want)
	}
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func names(ss []ExtractedSkill) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Canonical
	}
	return out
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestExtractSkills_AmbiguousWordsNeedContext is a regression test for a defect
// found in production data.
//
// A "Mid Market Account Executive" posting was scored a 96% match for a backend
// engineer. The cause was one word: "go to market" registered as the Go
// programming language, and skills are 40% of the score, so a single false
// token carried an entire sales role into the strong-match band.
//
// Word boundaries do not help here — "go" in "go to market" is a whole word.
// The distinguishing signal is context.
func TestExtractSkills_AmbiguousWordsNeedContext(t *testing.T) {
	v := DefaultVocabulary()

	hasSkill := func(text, skill string) bool {
		for _, s := range v.ExtractSkills(text) {
			if s.Canonical == skill {
				return true
			}
		}
		return false
	}

	// --- English usage that must NOT register as a skill ---
	englishUses := []struct{ name, text string }{
		{"go to market", "Requirements\nYou will own our go to market strategy across the region and drive revenue"},
		{"go above and beyond", "Requirements\nYou go above and beyond for every customer you work with"},
		{"spring the season", "About us\nWe are hiring for a start date in spring next year"},
		{"swift the network", "Requirements\nFamiliarity with SWIFT payment rails and correspondent banking is helpful"},
		{"rust the metal", "About\nWe replaced rust belt manufacturing lines with modern automation systems"},
	}
	for _, tc := range englishUses {
		skill := map[string]string{
			"go to market": "go", "go above and beyond": "go",
			"spring the season": "spring", "swift the network": "swift",
			"rust the metal": "rust",
		}[tc.name]
		if hasSkill(tc.text, skill) {
			t.Errorf("%s: extracted %q from ordinary English — this is the bug that "+
				"scored a sales role 96%% for a backend engineer", tc.name, skill)
		}
	}

	// --- Genuine references that MUST still register ---
	technicalUses := []struct{ name, text, skill string }{
		{"unambiguous alias", "Requirements\nDeep experience with Golang in production", "go"},
		{"qualifying noun", "About\nWe are looking for a Go engineer to join the platform team", "go"},
		{"preposition of use", "About the role\nOur services are written in Go and deployed on Kubernetes", "go"},
		{"terse bullet", "Requirements\nStrong Go", "go"},
		{"version number", "About\nThe codebase targets Go 1.22 across all services", "go"},
		{"spring boot", "Requirements\nSpring Boot microservices at scale", "spring"},
		{"swift for ios", "Requirements\nBuilding iOS applications in Swift", "swift"},
		{"rust with preposition", "About\nOur data plane is written in Rust for throughput", "rust"},
	}
	for _, tc := range technicalUses {
		if !hasSkill(tc.text, tc.skill) {
			t.Errorf("%s: failed to extract %q from a genuine technical reference — "+
				"the ambiguity fix must not cost real matches", tc.name, tc.skill)
		}
	}
}

// The terse-bullet allowance must not become a loophole: a long sentence under
// a requirements heading is still prose, and prose does not corroborate.
func TestExtractSkills_TerseAllowanceDoesNotCoverProse(t *testing.T) {
	v := DefaultVocabulary()

	long := "Requirements\nYou will go the extra mile to make sure every single customer is delighted"
	for _, s := range v.ExtractSkills(long) {
		if s.Canonical == "go" {
			t.Error(`extracted "go" from a long requirements sentence — the terse-line ` +
				`allowance is meant for skill bullets, not sentences`)
		}
	}
}

// The vocabulary grew from 47 to ~120 terms per ADR-0009, and every ordinary
// English word added to it is a new chance to repeat the "go to market" defect.
// These are the ones most likely to appear in company boilerplate.
func TestExtractSkills_NewVocabularyKeepsItsAmbiguityRules(t *testing.T) {
	v := DefaultVocabulary()

	has := func(text, skill string) bool {
		for _, s := range v.ExtractSkills(text) {
			if s.Canonical == skill {
				return true
			}
		}
		return false
	}

	// --- English that must NOT register ---
	english := []struct{ name, text, skill string }{
		{"spark joy", "About us\nWe spark joy in everything we build for our customers", "spark"},
		{"spark innovation", "About us\nOur mission is to spark innovation across the industry", "spark"},
		{"at the helm", "About\nWith a new CEO at the helm we are entering our next chapter", "helm"},
		{"job security", "Benefits\nWe offer job security and a competitive package", "security"},
		{"take security seriously", "About\nWe take security seriously across the company", "security"},
	}
	for _, tc := range english {
		if has(tc.text, tc.skill) {
			t.Errorf("%s: extracted %q from ordinary English", tc.name, tc.skill)
		}
	}

	// --- Genuine technical references that MUST register ---
	technical := []struct{ name, text, skill string }{
		{"apache spark", "Requirements\nExperience with Apache Spark at scale", "spark"},
		{"pyspark", "Requirements\nStrong PySpark and Airflow background", "spark"},
		{"helm charts", "Requirements\nYou write Helm charts for Kubernetes deployments", "helm"},
		{"application security", "Requirements\nBackground in application security", "security"},
		{"security engineer", "About\nJoin us as a security engineer on the platform team", "security"},
		{"airflow", "Requirements\nAirflow", "airflow"},
		{"pytorch", "Requirements\nPyTorch", "pytorch"},
		{"llm alias", "Requirements\nExperience shipping large language model features", "llm"},
	}
	for _, tc := range technical {
		if !has(tc.text, tc.skill) {
			t.Errorf("%s: failed to extract %q from a genuine reference", tc.name, tc.skill)
		}
	}
}

// Two aliases were deliberately NOT added because they collide with terms we
// already track or with ordinary code vocabulary. Recorded as tests so nobody
// helpfully adds them back.
func TestExtractSkills_DeliberatelyOmittedAliases(t *testing.T) {
	v := DefaultVocabulary()

	// "tf" is Terraform shorthand far more often than TensorFlow.
	for _, s := range v.ExtractSkills("Requirements\nExperience with tf modules") {
		if s.Canonical == "tensorflow" {
			t.Error(`"tf" resolved to tensorflow; it is Terraform shorthand in practice`)
		}
	}

	// A bare "lambda" is an anonymous function at least as often as it is AWS.
	for _, s := range v.ExtractSkills("Requirements\nComfortable with lambda expressions in Java") {
		if s.Canonical == "serverless" {
			t.Error(`bare "lambda" resolved to serverless; it is also an anonymous function`)
		}
	}
}

// Every string here was a live posting with country NULL on 2026-09-01.
//
// Grouped by the reason the parser missed them, because the fix for each was
// different: a country table that knew nine countries, and a region that was
// computed and then discarded.
func TestParseLocation_RecoversTheCountriesWeWereMissing(t *testing.T) {
	cases := map[string]string{
		// The country was spelled out in the last segment and the table had no
		// entry. 71% of the gap, and 451 postings from China alone.
		"Shanghai, Shanghai, China": "CN",
		"Suzhou, Jiangsu, China":    "CN",
		"Budapest, , Hungary":       "HU",
		"Tokyo, Japan":              "JP",
		"Braga, Braga, Portugal":    "PT",
		"Bangkok, Thailand":         "TH",

		// A state or province code, computed as a Region and then thrown away
		// because the result carried no city the curated list knew.
		"Washington, DC": "US",
		"Vancouver, BC":  "CA",
	}
	for raw, want := range cases {
		if got := ParseLocation(raw); got.Country != want {
			t.Errorf("ParseLocation(%q).Country = %q, want %q", raw, got.Country, want)
		}
	}
}

// The omissions from regionCountry are the load-bearing part.
//
// CA is California and Canada, DE is Delaware and Germany, IN is Indiana and
// India. Resolving a bare code for any of them would put a wrong country on a
// filter people rely on, and a wrong country is worse than an absent one.
func TestParseLocation_DoesNotGuessAnAmbiguousRegionCode(t *testing.T) {
	if c := ParseLocation("Munich, DE").Country; c == "DE" {
		t.Error("a bare DE was read as Germany; it is also Delaware")
	}
	if c := ParseLocation("Springfield, IN").Country; c == "IN" {
		t.Error("a bare IN was read as India; it is also Indiana")
	}
	// The spelled-out name is unambiguous and must still resolve.
	if c := ParseLocation("Munich, Germany").Country; c != "DE" {
		t.Errorf("ParseLocation(\"Munich, Germany\") = %q, want DE", c)
	}
}

// A string with no country in it must stay empty. "Remote" is 6.7% of the gap
// and there is nothing in it to find.
func TestParseLocation_StillAbstainsOnAPlacelessString(t *testing.T) {
	for _, raw := range []string{"Remote", "Hybrid", "In-Office"} {
		if c := ParseLocation(raw).Country; c != "" {
			t.Errorf("ParseLocation(%q).Country = %q, want empty", raw, c)
		}
	}
	_ = strings.TrimSpace("")
}

// Every string here was among the most frequent live locations with no country
// on 2026-10-05, after the corpus was rebuilt.
func TestParseLocation_RecoversTheFormatsMissedOnTheRebuiltCorpus(t *testing.T) {
	cases := map[string]string{
		// A vendor format: ISO country, optional region, city.
		"US-CA-Menlo Park":                  "US",
		"US-CA-Menlo Park / US-WA-Bellevue": "US",
		"GB-London":                         "GB",
		"JP-Tokyo":                          "JP",
		// The country, wrapped in the arrangement.
		"United States (Remote)":        "US",
		"Canada (Remote)":               "CA",
		"Remote: United States":         "US",
		"Remote - US: Select locations": "US",
		"*HQ - San Francisco, CA":       "US",
		// A spelled-out state or province.
		"Mountain View, California": "US",
		"Bellevue, Washington":      "US",
		// Cities the table did not know.
		"Foster City, CA":        "US",
		"Chicago":                "US",
		"San Francisco Bay Area": "US",
		"Mexico City":            "MX",
		"Munich":                 "DE",
		"Paris":                  "FR",
		"São Paulo":              "BR",
		// A namesake beside a region code is the region's: Texas, not France.
		"Paris, TX": "US",
	}
	for raw, want := range cases {
		if got := ParseLocation(raw); got.Country != want {
			t.Errorf("ParseLocation(%q).Country = %q, want %q", raw, got.Country, want)
		}
	}
}

// Wider tables must not start guessing. Each of these names no single country,
// or names one only by a reading that could be wrong.
func TestParseLocation_TheNewRulesStillAbstain(t *testing.T) {
	for _, raw := range []string{
		"Warsaw, IN",    // Indiana
		"Georgia",       // a state and a country
		"XY-Somewhere",  // not a code the table can produce
		"EMEA",          // a region of the world
		"North America", // two countries at least
		"Distributed",   // no place at all
	} {
		if c := ParseLocation(raw).Country; c != "" {
			t.Errorf("ParseLocation(%q).Country = %q, want empty", raw, c)
		}
	}
}
