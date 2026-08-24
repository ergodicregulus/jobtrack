package resume

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jobtrack/jobtrack/internal/normalise"
)

// ParserVersion is stamped on every parse.
//
// Stored on the row so a parser improvement can re-run over existing resumes,
// exactly as the scorer's version drives its own sweep. Bump it whenever the
// output would differ for the same input.
const ParserVersion = "2026.08.1"

// Result is everything we understood from one CV, plus how sure we are.
//
// Every field is a proposal the user can overrule. Nothing here is written to a
// profile without being shown first — a silently wrong skill list corrupts
// every score downstream while looking like it worked.
type Result struct {
	Format Format `json:"format"`

	Email string   `json:"email,omitempty"`
	Phone string   `json:"phone,omitempty"`
	Links []string `json:"links,omitempty"`

	// Skills carry where in the CV they were found. A skill under the Skills
	// heading is a claim; the same word inside a job description is evidence of
	// use, which is stronger. The UI shows the difference.
	Skills []FoundSkill `json:"skills"`

	// YearsOfExperience is derived from date ranges in the experience section,
	// and is nil when we could not find any. Nil is not zero: "we could not
	// tell" and "no experience" are opposite claims about a candidate.
	YearsOfExperience *float64 `json:"years_of_experience"`

	// Confidence is how much of the document we actually understood, 0–1.
	Confidence float64 `json:"confidence"`

	// Diagnostics is the user-facing explanation of what we could not read and
	// why. It is a product feature, not debug output: if we mangled the file,
	// an employer's ATS will mangle it too, and that is worth knowing.
	Diagnostics []Diagnostic `json:"diagnostics"`

	ParserVersion string `json:"parser_version"`
}

// FoundSkill is one skill and the evidence for it.
type FoundSkill struct {
	Canonical string `json:"canonical"`
	// Evidence is where it was found: "skills" (listed), "experience" (used in
	// a role), "projects", or "summary".
	Evidence string `json:"evidence"`
	// Years is nil unless the CV stated one, e.g. "Go (5 years)". Never
	// inferred from the document's date ranges — that would attribute total
	// career length to every individual skill.
	Years *float64 `json:"years,omitempty"`
}

// Diagnostic is one thing we could not read, in the user's terms.
type Diagnostic struct {
	// Code is stable and machine-readable; Message is what a person reads.
	Code    string `json:"code"`
	Message string `json:"message"`
	// Severity: "blocking" means the parse is unusable, "warning" means it
	// worked but something is likely missing.
	Severity string `json:"severity"`
}

var (
	// Deliberately conservative. A pattern that matches too much writes a wrong
	// email into a profile, and a wrong contact detail is worse than a missing
	// one because the user has no reason to check it.
	emailRe = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)

	// Requires a separator or a leading +, so a bare 10-digit string — an
	// employee number, a postcode, a year range without a dash — is not read as
	// a phone number.
	phoneRe = regexp.MustCompile(`(?:\+\d{1,3}[ .\-]?)?(?:\(\d{2,4}\)[ .\-]?)?\d{3,5}[ .\-]\d{3,5}(?:[ .\-]\d{2,5})?`)

	linkRe = regexp.MustCompile(`(?i)\b(?:https?://)?(?:www\.)?(github\.com|gitlab\.com|linkedin\.com|stackoverflow\.com)/[\w\-./]+`)

	// Date ranges in an experience section. "2019 - 2023", "Jan 2019 – Present",
	// "03/2019 to 06/2021".
	monthNames  = `jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t|tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?`
	dateRangeRe = regexp.MustCompile(`(?i)\b(?:(` + monthNames + `)[a-z]*\.?\s+)?(\d{4})\s*(?:-|to|until|through)\s*(?:(` + monthNames + `)[a-z]*\.?\s+)?(\d{4}|present|current|now|ongoing)\b`)

	// "5 years", "5+ years", "5-7 years" stated outright in a summary.
	statedYearsRe = regexp.MustCompile(`(?i)\b(\d{1,2})\s*\+?\s*(?:-\s*\d{1,2}\s*)?(?:years?|yrs?)\b`)

	// "Go (5 years)" or "Python — 3 yrs" beside a skill.
	skillYearsRe = regexp.MustCompile(`(?i)[(\-–—,]\s*(\d{1,2})\s*\+?\s*(?:years?|yrs?)`)
)

// Parse turns extracted text into a Result.
//
// Split from [Extract] so every rule below is testable against a string. The
// binary formats need fixtures; the understanding does not, and coupling them
// would mean a new .docx for every heading variant worth covering.
func Parse(text string, format Format, vocab *normalise.Vocabulary) Result {
	r := Result{
		Format:        format,
		ParserVersion: ParserVersion,
		Skills:        []FoundSkill{},
		Diagnostics:   []Diagnostic{},
	}

	lines := Classify(text)
	if len(lines) == 0 {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{
			Code:     "empty",
			Severity: "blocking",
			Message:  "We found no text in this file at all.",
		})
		return r
	}

	head := TextOf(lines, SectionContact, SectionSummary)
	r.Email = emailRe.FindString(head)
	if r.Email == "" {
		// A CV without a contact block usually means the layout defeated us —
		// most often a two-column design where the sidebar was interleaved.
		r.Email = emailRe.FindString(text)
	}
	r.Phone = strings.TrimSpace(phoneRe.FindString(head))
	r.Links = dedupe(linkRe.FindAllString(text, 8))

	r.Skills = extractSkills(lines, vocab)
	r.YearsOfExperience = yearsOfExperience(lines)
	r.Diagnostics = append(r.Diagnostics, diagnose(lines, r)...)
	r.Confidence = confidence(lines, r)

	return r
}

// extractSkills runs the vocabulary over each section separately.
//
// Per section rather than over the whole document, because WHERE a skill
// appears is the strongest signal available about it. "Kubernetes" under a
// Skills heading is a claim; "migrated the platform to Kubernetes" inside a
// role is evidence of use. Losing that distinction would mean treating a
// buzzword list and a career's work as the same thing.
func extractSkills(lines []Line, vocab *normalise.Vocabulary) []FoundSkill {
	// Ranked weakest to strongest, so a later section overwrites an earlier one
	// and each skill keeps its best evidence.
	order := []struct {
		section  Section
		evidence string
	}{
		// The preamble is scanned too, at the weakest evidence level. It is
		// where everything lands when a CV uses headings we do not recognise,
		// and skipping it meant such a document yielded ZERO skills while being
		// perfectly readable — a far more common failure than the mangled
		// layouts this section ordering was designed around. In a conventional
		// CV the preamble is three lines, so this costs nothing there.
		{SectionContact, "unlabelled"},
		{SectionOther, "other"},
		{SectionEducation, "education"},
		{SectionSummary, "summary"},
		{SectionSkills, "skills"},
		{SectionProjects, "projects"},
		{SectionExperience, "experience"},
	}

	best := map[string]FoundSkill{}
	for _, o := range order {
		body := TextOf(lines, o.section)
		if strings.TrimSpace(body) == "" {
			continue
		}
		for _, s := range vocab.ExtractSkills(body) {
			found := FoundSkill{Canonical: s.Canonical, Evidence: o.evidence}
			// Only a year the CV states outright, and only next to the skill.
			if y := statedSkillYears(body, s.Canonical); y != nil {
				found.Years = y
			}
			best[s.Canonical] = found
		}
	}

	out := make([]FoundSkill, 0, len(best))
	for _, v := range best {
		out = append(out, v)
	}
	// Deterministic order. A parser whose output permutes between runs cannot
	// have a golden test, and this one is required to.
	sort.Slice(out, func(i, j int) bool { return out[i].Canonical < out[j].Canonical })
	return out
}

// statedSkillYears looks for "Go (5 years)" on the same line as the skill.
func statedSkillYears(body, canonical string) *float64 {
	for _, line := range strings.Split(body, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, canonical) {
			continue
		}
		// Only after the skill name, so "5 years with Java, plus Go" does not
		// attribute five years to Go.
		idx := strings.Index(lower, canonical)
		m := skillYearsRe.FindStringSubmatch(lower[idx:])
		if m == nil {
			continue
		}
		if n, err := strconv.ParseFloat(m[1], 64); err == nil && n > 0 && n < 60 {
			return &n
		}
	}
	return nil
}

// yearsOfExperience derives total professional experience from date ranges.
//
// Union of the ranges, not a sum: two roles held simultaneously (a job and a
// contract) are one span of time, and summing them invents years the person
// does not have. That is exactly the kind of flattering error a CV parser must
// not make.
func yearsOfExperience(lines []Line) *float64 {
	body := TextOf(lines, SectionExperience)
	if strings.TrimSpace(body) == "" {
		// Some CVs state it directly in the summary instead. Weaker evidence,
		// but stated by the candidate, which beats inferring nothing.
		if m := statedYearsRe.FindStringSubmatch(TextOf(lines, SectionSummary)); m != nil {
			if n, err := strconv.ParseFloat(m[1], 64); err == nil && n > 0 && n < 60 {
				return &n
			}
		}
		return nil
	}

	type span struct{ from, to float64 }
	var spans []span
	thisYear := float64(time.Now().Year())

	for _, m := range dateRangeRe.FindAllStringSubmatch(body, -1) {
		from, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		if m[1] != "" {
			from += monthFraction(m[1])
		}

		var to float64
		switch strings.ToLower(m[4]) {
		case "present", "current", "now", "ongoing":
			to = thisYear + monthFraction(time.Now().Month().String())
		default:
			to, err = strconv.ParseFloat(m[4], 64)
			if err != nil {
				continue
			}
			if m[3] != "" {
				to += monthFraction(m[3])
			}
		}

		// Reject nonsense rather than letting it through: a typo'd "2109" would
		// otherwise report 80 years of experience.
		if to < from || from < 1950 || to > thisYear+1 {
			continue
		}
		spans = append(spans, span{from, to})
	}
	if len(spans) == 0 {
		return nil
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
	total := 0.0
	cur := spans[0]
	for _, s := range spans[1:] {
		if s.from <= cur.to { // overlapping or contiguous: extend, do not add
			if s.to > cur.to {
				cur.to = s.to
			}
			continue
		}
		total += cur.to - cur.from
		cur = s
	}
	total += cur.to - cur.from

	if total <= 0 {
		return nil
	}
	total = float64(int(total*10+0.5)) / 10
	return &total
}

func monthFraction(name string) float64 {
	months := map[string]float64{
		"jan": 0, "feb": 1, "mar": 2, "apr": 3, "may": 4, "jun": 5,
		"jul": 6, "aug": 7, "sep": 8, "oct": 9, "nov": 10, "dec": 11,
	}
	key := strings.ToLower(name)
	if len(key) > 3 {
		key = key[:3]
	}
	return months[key] / 12
}

// diagnose explains what we could not read, in terms a person can act on.
func diagnose(lines []Line, r Result) []Diagnostic {
	var out []Diagnostic

	sections := map[Section]int{}
	for _, l := range lines {
		if !l.Heading {
			sections[l.Section]++
		}
	}

	if sections[SectionExperience] == 0 {
		out = append(out, Diagnostic{
			Code:     "no_experience_section",
			Severity: "warning",
			Message: "We could not find a work-experience section. If your CV uses a " +
				"two-column layout, the columns may have been interleaved — that " +
				"defeats most employer systems too, not just ours.",
		})
	}
	if len(r.Skills) == 0 {
		out = append(out, Diagnostic{
			Code:     "no_skills",
			Severity: "warning",
			Message: "We recognised no technologies in this CV. Add them below and " +
				"they will be used for matching straight away.",
		})
	}
	if r.Email == "" {
		out = append(out, Diagnostic{
			Code:     "no_contact",
			Severity: "warning",
			Message: "We could not find an email address. That usually means the " +
				"contact details are in a header, a text box or an image, which " +
				"applicant tracking systems routinely miss.",
		})
	}
	if r.YearsOfExperience == nil && sections[SectionExperience] > 0 {
		out = append(out, Diagnostic{
			Code:     "no_dates",
			Severity: "warning",
			Message: "We found your roles but could not read their dates, so years " +
				"of experience is not set. Dates like “Jan 2020 – Mar 2023” parse " +
				"most reliably.",
		})
	}
	return out
}

// confidence is the share of a CV's expected structure we actually recovered.
//
// Deliberately structural rather than a feeling. Each check is a thing a CV
// reliably has, so a low score means specific missing pieces the diagnostics
// then name — the number and the explanation cannot disagree.
func confidence(lines []Line, r Result) float64 {
	sections := map[Section]int{}
	for _, l := range lines {
		if !l.Heading {
			sections[l.Section]++
		}
	}

	checks := []struct {
		got    bool
		weight float64
	}{
		{r.Email != "", 0.15},
		{len(r.Skills) > 0, 0.30},
		{sections[SectionExperience] > 0, 0.25},
		{r.YearsOfExperience != nil, 0.15},
		{sections[SectionEducation] > 0, 0.05},
		{len(lines) >= 15, 0.10}, // a CV shorter than this is a fragment
	}

	total := 0.0
	for _, c := range checks {
		if c.got {
			total += c.weight
		}
	}
	return float64(int(total*100+0.5)) / 100
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		key := strings.ToLower(strings.TrimRight(s, "/"))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
