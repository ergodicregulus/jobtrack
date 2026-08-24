package resume

import (
	"regexp"
	"strings"
)

// Section is a region of a CV, classified by the heading above it.
type Section string

const (
	SectionContact    Section = "contact"
	SectionSummary    Section = "summary"
	SectionSkills     Section = "skills"
	SectionExperience Section = "experience"
	SectionEducation  Section = "education"
	SectionProjects   Section = "projects"
	SectionOther      Section = "other"
)

// sectionHeadings maps a heading to what follows it.
//
// Anchored with ^...$ against the whole trimmed line, unlike the job-posting
// equivalent which matches a prefix. A CV heading IS the line — "Skills" alone
// on a row — whereas a posting writes "What we're looking for:" mid-prose. A
// prefix match here would classify the sentence "Skills I have picked up along
// the way include…" as a heading and swallow everything after it.
var sectionHeadings = []struct {
	re      *regexp.Regexp
	section Section
}{
	{regexp.MustCompile(`(?i)^(technical\s+)?skills?(\s*(&|and)\s*(tools?|technolog\w+|competenc\w+))?:?$`), SectionSkills},
	{regexp.MustCompile(`(?i)^(technolog\w+|tech\s+stack|tooling|core\s+competenc\w+|areas\s+of\s+expertise|proficienc\w+):?$`), SectionSkills},
	{regexp.MustCompile(`(?i)^(work\s+|professional\s+|employment\s+|relevant\s+)?(experience|history|background)(\s*(&|and)\s*\w+)?:?$`), SectionExperience},
	{regexp.MustCompile(`(?i)^(career|positions?\s+held|work)$`), SectionExperience},
	{regexp.MustCompile(`(?i)^education(\s*(&|and)\s*\w+)?:?$`), SectionEducation},
	{regexp.MustCompile(`(?i)^(academic\s+(background|qualifications?)|qualifications?|degrees?|certifications?|training):?$`), SectionEducation},
	{regexp.MustCompile(`(?i)^(personal\s+|side\s+|selected\s+)?projects?:?$`), SectionProjects},
	{regexp.MustCompile(`(?i)^(open\s+source|portfolio|publications?|research):?$`), SectionProjects},
	{regexp.MustCompile(`(?i)^(summary|profile|about(\s+me)?|objective|professional\s+summary|career\s+objective):?$`), SectionSummary},
	{regexp.MustCompile(`(?i)^(contact(\s+(details?|info\w*))?|details|personal\s+(details|information)):?$`), SectionContact},
	{regexp.MustCompile(`(?i)^(awards?|achievements?|interests?|hobbies|languages?|references?|volunteer\w*|activities):?$`), SectionOther},
}

// headingMaxWords bounds what can be a heading.
//
// A heading is short. Without this, a bullet like "Experience building
// distributed systems at scale across three teams" matches the experience
// pattern and re-classifies the rest of the document — which is the failure
// that makes a parser look randomly broken rather than wrong in a fixable way.
const headingMaxWords = 6

// Line is one line of a CV with the section it belongs to.
type Line struct {
	Text    string
	Section Section
	// Heading marks the line as the heading itself rather than content under
	// it. Headings are excluded from skill extraction: the word "Skills" is not
	// a skill, and a section called "Go-to-market" is not the Go language.
	Heading bool
}

// Classify assigns every line of a CV to a section.
//
// Everything before the first recognised heading is contact/summary territory:
// a CV opens with a name, an email and often a one-line pitch, none of which
// sits under a heading. Calling that "other" would throw away the block that
// reliably contains the contact details.
func Classify(text string) []Line {
	raw := strings.Split(text, "\n")
	lines := make([]Line, 0, len(raw))

	current := SectionContact
	seenHeading := false

	for _, r := range raw {
		trimmed := strings.TrimSpace(r)
		if trimmed == "" {
			continue
		}

		if s, ok := headingFor(trimmed); ok {
			current = s
			seenHeading = true
			lines = append(lines, Line{Text: trimmed, Section: s, Heading: true})
			continue
		}

		// The preamble stops being contact material once it stops looking like
		// it — a name and an email are short, a paragraph is a summary.
		if !seenHeading && current == SectionContact && len(strings.Fields(trimmed)) > 12 {
			current = SectionSummary
		}

		lines = append(lines, Line{Text: trimmed, Section: current})
	}
	return lines
}

func headingFor(line string) (Section, bool) {
	if len(strings.Fields(line)) > headingMaxWords {
		return "", false
	}
	// Trailing colons and decorative underlines are common and carry no
	// meaning. Stripping them here keeps every pattern above simpler.
	clean := strings.TrimRight(line, ":—–-_= \t")
	if clean == "" {
		return "", false
	}
	for _, h := range sectionHeadings {
		if h.re.MatchString(clean) {
			return h.section, true
		}
	}
	return "", false
}

// TextOf joins the lines of one section, excluding its heading.
func TextOf(lines []Line, sections ...Section) string {
	want := make(map[Section]bool, len(sections))
	for _, s := range sections {
		want[s] = true
	}
	var b strings.Builder
	for _, l := range lines {
		if l.Heading || !want[l.Section] {
			continue
		}
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.String()
}
