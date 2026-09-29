package resume

import (
	"strings"
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/normalise"
)

func vocab() *normalise.Vocabulary { return normalise.DefaultVocabulary() }

// A conventional single-column CV. Everything downstream is measured against
// what this one produces.
const goodCV = `Priya Raman
priya.raman@example.com | +91 98765 43210
github.com/priyaraman | linkedin.com/in/priyaraman

Summary
Backend engineer working on payments infrastructure and distributed systems.

Skills
Go, PostgreSQL, Kubernetes, Docker, Terraform, Kafka
Python, gRPC, Redis

Experience
Senior Software Engineer, Razorpay
Mar 2021 - Present
Built the settlement ledger in Go on top of PostgreSQL, processing 4M events daily.
Migrated the deployment pipeline to Kubernetes and cut release time by half.

Software Engineer, Freshworks
Jul 2018 - Feb 2021
Owned the billing service. Introduced Kafka for event delivery.

Education
B.Tech, Computer Science, Anna University
2014 - 2018
`

func skillNames(r Result) []string {
	out := make([]string, 0, len(r.Skills))
	for _, s := range r.Skills {
		out = append(out, s.Canonical)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestParse_ReadsAConventionalCV(t *testing.T) {
	r := Parse(goodCV, FormatText, vocab())

	if r.Email != "priya.raman@example.com" {
		t.Errorf("email = %q, want priya.raman@example.com", r.Email)
	}

	// The whole justification for this phase: a hand-typed profile has about
	// five skills, and a parsed CV should comfortably beat that.
	got := skillNames(r)
	if len(got) < 8 {
		t.Errorf("found %d skills %v, want at least 8 — the phase exists because "+
			"a parsed CV should beat a hand-typed five", len(got), got)
	}
	for _, want := range []string{"go", "postgresql", "kubernetes", "kafka", "python"} {
		if !has(got, want) {
			t.Errorf("missing %q from %v", want, got)
		}
	}

	// Mar 2021–now plus Jul 2018–Feb 2021 is one continuous span from 2018.
	if r.YearsOfExperience == nil {
		t.Fatal("years of experience is nil for a CV with two dated roles")
	}
	if *r.YearsOfExperience < 6 || *r.YearsOfExperience > 12 {
		t.Errorf("years = %.1f, want a continuous span from mid-2018", *r.YearsOfExperience)
	}

	if r.Confidence < 0.8 {
		t.Errorf("confidence = %.2f on a clean CV with every section present", r.Confidence)
	}
}

// Where a skill appears is the strongest signal available about it, and losing
// that distinction would treat a buzzword list and a career's work alike.
func TestParse_KeepsTheStrongestEvidenceForASkill(t *testing.T) {
	r := Parse(goodCV, FormatText, vocab())

	byName := map[string]FoundSkill{}
	for _, s := range r.Skills {
		byName[s.Canonical] = s
	}

	// Go and Kubernetes are both listed under Skills AND used in a role. Used
	// in a role is the stronger claim and must win.
	for _, name := range []string{"go", "kubernetes"} {
		if byName[name].Evidence != "experience" {
			t.Errorf("%s evidence = %q, want experience — it is described in a role, "+
				"not merely listed", name, byName[name].Evidence)
		}
	}

	// Terraform appears only in the list, so it must not claim to be more.
	if e := byName["terraform"].Evidence; e != "skills" {
		t.Errorf("terraform evidence = %q, want skills — it is only listed", e)
	}
}

// Two roles held at the same time are one span of time. Summing them invents
// years the candidate does not have, which is exactly the flattering error a
// CV parser must never make.
func TestParse_OverlappingRolesAreNotSummed(t *testing.T) {
	cv := `Alex Doe
alex@example.com

Experience
Staff Engineer, Acme
Jan 2019 - Jan 2024
Backend work in Go.

Contract Engineer, Side Project Ltd
Jan 2020 - Jan 2022
More Go.

Skills
Go
`
	r := Parse(cv, FormatText, vocab())
	if r.YearsOfExperience == nil {
		t.Fatal("years is nil")
	}
	// The union is 2019–2024, five years. Summing would give seven.
	if *r.YearsOfExperience > 5.5 {
		t.Errorf("years = %.1f; overlapping roles were summed rather than unioned "+
			"(union is 5, sum would be 7)", *r.YearsOfExperience)
	}
}

// The ADR asks for a deliberately broken fixture so the failure path is visible
// during development rather than only in production. This is a two-column CV
// linearised naively: the sidebar interleaves with the body, which is the
// single most common cause of catastrophic parse failure.
func TestParse_InterleavedTwoColumnCVFailsLoudly(t *testing.T) {
	broken := `Priya Raman	Summary
priya@example.com	Backend engineer working on
+91 98765 43210	payments infrastructure.
	Senior Software Engineer, Razorpay
SKILLS	Mar 2021 - Present
Go	Built the settlement ledger in Go.
PostgreSQL	Software Engineer, Freshworks
Kubernetes	Jul 2018 - Feb 2021
`
	r := Parse(broken, FormatText, vocab())

	// It must not pretend this went well. The number is what the UI shows, and
	// a confident wrong parse is worse than an honest failed one.
	if r.Confidence >= 0.8 {
		t.Errorf("confidence = %.2f on an interleaved two-column CV; a mangled "+
			"parse must not report near-certainty", r.Confidence)
	}

	// And it must say something the user can act on, naming the likely cause.
	var codes []string
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	if len(r.Diagnostics) == 0 {
		t.Fatal("no diagnostics for a CV we clearly struggled with")
	}

	joined := strings.ToLower(strings.Join(diagnosticMessages(r), " "))
	if !strings.Contains(joined, "column") && !strings.Contains(joined, "date") {
		t.Errorf("diagnostics %v name no likely cause; the diagnostic view is a "+
			"product feature, not a shrug", codes)
	}
}

func diagnosticMessages(r Result) []string {
	out := make([]string, 0, len(r.Diagnostics))
	for _, d := range r.Diagnostics {
		out = append(out, d.Message)
	}
	return out
}

// nil and 0 are opposite claims about a candidate, and the type carries the
// difference precisely so this cannot be flattened by accident.
func TestParse_UnknownYearsIsNilNotZero(t *testing.T) {
	cv := `Sam Patel
sam@example.com

Skills
Go, Python
`
	r := Parse(cv, FormatText, vocab())
	if r.YearsOfExperience != nil {
		t.Errorf("years = %v for a CV with no dates at all; nil means "+
			"'we could not tell', 0 would mean 'no experience'", *r.YearsOfExperience)
	}
}

// A heading is a heading, not content. "Go-to-market Strategy" as a section
// title must not put Go in someone's skill list.
func TestParse_HeadingsAreNotSkills(t *testing.T) {
	cv := `Jo Smith
jo@example.com

Go-to-market Strategy
Led positioning for three product launches.

Experience
Marketing Manager, Acme
Jan 2020 - Jan 2023
Ran campaigns and owned the funnel.
`
	r := Parse(cv, FormatText, vocab())
	if has(skillNames(r), "go") {
		t.Errorf("extracted Go from a go-to-market CV: %v", skillNames(r))
	}
}

// A CV that states its own years beats inferring nothing at all.
func TestParse_FallsBackToStatedYearsInASummary(t *testing.T) {
	cv := `Dana Lee
dana@example.com

Summary
Platform engineer with 9 years of experience across infrastructure teams.

Skills
Terraform, Kubernetes
`
	r := Parse(cv, FormatText, vocab())
	if r.YearsOfExperience == nil {
		t.Fatal("years is nil despite the summary stating it outright")
	}
	if *r.YearsOfExperience != 9 {
		t.Errorf("years = %.1f, want 9", *r.YearsOfExperience)
	}
}

// The parse must be byte-identical between runs or a golden corpus is
// impossible, and ADR-0007 requires one.
func TestParse_IsDeterministic(t *testing.T) {
	first := Parse(goodCV, FormatText, vocab())
	for i := 0; i < 5; i++ {
		next := Parse(goodCV, FormatText, vocab())
		if len(next.Skills) != len(first.Skills) {
			t.Fatalf("skill count changed between runs: %d then %d",
				len(first.Skills), len(next.Skills))
		}
		for j := range next.Skills {
			if next.Skills[j] != first.Skills[j] {
				t.Fatalf("skill order changed at %d: %v vs %v",
					j, first.Skills[j], next.Skills[j])
			}
		}
		if next.Confidence != first.Confidence {
			t.Fatalf("confidence changed between runs: %v then %v",
				first.Confidence, next.Confidence)
		}
	}
}

func TestParse_RejectsNonsenseDates(t *testing.T) {
	cv := `Chris Fox
chris@example.com

Experience
Engineer, Acme
2109 - 2110
Did some work.

Skills
Go
`
	r := Parse(cv, FormatText, vocab())
	if r.YearsOfExperience != nil {
		t.Errorf("years = %.1f from a typo'd future date range; nonsense must be "+
			"dropped rather than reported", *r.YearsOfExperience)
	}
}

// A CV using headings we do not recognise must still yield skills.
//
// Found by looking at the two-column fixture's output: everything landed in the
// unclassified preamble, which extractSkills was not scanning, so a perfectly
// readable document produced ZERO skills. That is a far more common failure
// than the mangled layouts the section ordering was designed around — plenty of
// people write "Tech I've used" or "Toolbox" instead of "Skills".
func TestParse_UnrecognisedHeadingsStillYieldSkills(t *testing.T) {
	cv := `Robin Shaw
robin@example.com

What I bring to the table
Go, PostgreSQL and Kubernetes, mostly on payments systems.

Where I have been
Acme, 2019 - 2024. Backend services in Go.
`
	r := Parse(cv, FormatText, vocab())

	got := skillNames(r)
	for _, want := range []string{"go", "postgresql", "kubernetes"} {
		if !has(got, want) {
			t.Errorf("missing %q from %v — an unrecognised heading must not "+
				"discard the whole document", want, got)
		}
	}

	// But the evidence must be honest about the fact that we could not tell
	// where in the CV these came from.
	for _, s := range r.Skills {
		if s.Evidence == "experience" {
			t.Errorf("%s claims experience evidence from an unlabelled section",
				s.Canonical)
		}
	}
}
