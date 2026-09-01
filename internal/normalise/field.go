package normalise

import "strings"

// Field is what kind of work a posting is, as far as we can tell.
//
// THE THREE VALUES ARE NOT SYMMETRIC, and the feed's default depends on that.
// The default view excludes `other` and keeps `software` AND `unknown`, so the
// two ways of being wrong cost very different things:
//
//   - Calling a software role `other` HIDES a job someone wanted. Expensive.
//   - Calling a non-software role `unknown` leaves it in the default view.
//     Cheap: the reader sees one posting that does not fit, and can say so.
//
// So the classifier claims `other` only on strong evidence and abstains
// readily. "Deal Lead, Robinhood Ventures" is plainly not software work and this
// still returns `unknown` for it, because nothing in the title is a token we
// recognise — and being wrong in that direction costs a reader almost nothing,
// where the opposite direction costs them the job.
//
// Three values, and the third is not a failure. Two-thirds of the corpus is not
// software work — one employer's board carries HVAC sales in Bogotá beside its
// platform roles — so a feed that claims to be for software engineers has to say
// which postings it believes are which, and admit where it does not know.
type Field string

const (
	FieldSoftware Field = "software"
	FieldOther    Field = "other"
	FieldUnknown  Field = "unknown"
)

// FieldGuess is a classification with its reason attached.
//
// Because is shown to the reader. A label the user can see the grounds for is
// one they can correct; a bare verdict is one they can only distrust, and this
// product's whole argument is that it shows its working.
type FieldGuess struct {
	Field      Field
	Confidence float64
	Because    string
}

// Title tokens that settle it, checked before anything else.
//
// A negative match wins over every other signal, and that ordering is the single
// most important thing here. "Sales Engineer" contains "engineer"; "Director,
// Strategic Finance" sat in the corpus with three recognised skills because the
// description mentioned tools. Skills cannot rescue a title that has already
// said what the job is.
var notSoftwareTitle = []string{
	"sales", "account executive", "account manager", "business development",
	"customer success", "customer service", "support specialist",
	"finance", "accounting", "accountant", "controller", "treasury", "payroll",
	"investor relations", "travel & expense", "procurement", "sourcing analyst",
	"legal", "counsel", "compliance officer",
	"recruiter", "recruiting", "talent acquisition", "people partner",
	"human resources", "hr business partner", "werkstudent people",
	"marketing", "brand", "communications", "content writer", "copywriter",
	"logistics", "warehouse", "driver", "facilities", "janitor",
	"nurse", "physician", "clinical",
	"food safety", "fsqa", "hvac", "electrician", "welder", "machinist",

	// Added after reading what actually landed in `unknown` on 2026-09-01.
	// Every phrase below is a real live title, not a guess: abstaining on
	// "UT Technician" and "Deal Desk Manager" left half the corpus unclassified
	// and the default feed no more relevant than before.
	//
	// The phrases are specific on purpose. A bare "manager" would swallow
	// "Engineering Manager", which is the job this product is for.
	"product manager", "program manager", "project manager", "product owner",
	"deal desk", "deal lead", "ventures", "value engineer",
	"solutions consultant", "account management", "supply chain",
	"technician", "coordinator", "video editor", "creative",
	"buyer", "planner", "auditor", "trainer", "instructor",

	// German and Spanish, because Bosch and the Personio tenants post in them
	// and an English-only list abstains on an entire employer.
	"techniker", "konstruktion", "vertrieb", "einkauf", "buchhaltung",
	"mechaniker", "elektro", "asesor comercial", "ventas",
}

// Title tokens that mean software on their own.
var softwareTitle = []string{
	"software", "backend", "back-end", "back end", "frontend", "front-end",
	"front end", "fullstack", "full-stack", "full stack",
	"developer", "programmer", "programming", "sre", "site reliability", "devops",
	"platform engineer", "infrastructure engineer", "data engineer",
	"machine learning", "ml engineer", "ai engineer", "applications engineer",
	"security engineer", "systems engineer", "mobile engineer",
	"android", "ios engineer", "web engineer", "qa engineer", "test engineer",
	"solutions architect", "software architect", "engineering manager",
	"engineering lead", "technical lead", "tech lead",
}

// Domains that make a bare "engineer" mean something other than software.
// Bosch alone posts development, process and quality engineers by the hundred.
var otherEngineeringDomain = []string{
	"mechanical", "electrical", "electronic", "civil", "chemical", "process",
	"manufacturing", "production", "industrial", "structural", "automotive",
	"hardware", "thermal", "acoustic", "materials", "packaging",
}

// skillsDecideFrom is where extracted skills become evidence on their own.
//
// Chosen from the corpus, not from intuition. At three recognised skills the
// population is still 17% software-titled — a finance director qualifies, because
// the description named three tools. The signal rises gradually and never
// separates cleanly: even at nine skills only 56% carry a software title. So this
// is deliberately high and is only ever a TIEBREAK, never a verdict on its own.
const skillsDecideFrom = 6

// ClassifyField decides what kind of work a posting is.
//
// Deterministic and explainable: a table of tokens, checked in an order chosen so
// that the most specific claim wins. No model, because a model here would be
// unauditable at exactly the moment a user disagrees with it, and because the
// vocabulary that extracts skills already does the harder half of the job.
//
// Abstains readily. FieldUnknown is the correct answer for a posting whose title
// says nothing we recognise and whose body we could not read, and it is a large
// bucket by design — the alternative is a confident guess, which is the failure
// ADR-0011 documents at length for the scorer.
func ClassifyField(title string, skillCount int) FieldGuess {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return FieldGuess{FieldUnknown, 0, "no title to read"}
	}

	if hit := firstMatch(t, notSoftwareTitle); hit != "" {
		return FieldGuess{FieldOther, 0.9, "the title says " + hit}
	}
	if hit := firstMatch(t, softwareTitle); hit != "" {
		return FieldGuess{FieldSoftware, 0.9, "the title says " + hit}
	}

	// A bare "engineer" is ambiguous, and on this corpus it is more often not
	// software than software.
	if strings.Contains(t, "engineer") {
		if hit := firstMatch(t, otherEngineeringDomain); hit != "" {
			return FieldGuess{FieldOther, 0.85, "a " + hit + " engineer"}
		}
		if skillCount >= skillsDecideFrom {
			return FieldGuess{FieldSoftware, 0.55, plural(skillCount) + " in the description"}
		}
		return FieldGuess{FieldUnknown, 0.2, "an engineer, but of what we cannot tell"}
	}

	// No title signal at all. Skills are the only evidence left, and they are
	// weak enough that this stays a low-confidence claim.
	if skillCount >= skillsDecideFrom {
		return FieldGuess{FieldSoftware, 0.45, plural(skillCount) + " in the description"}
	}
	return FieldGuess{FieldUnknown, 0, "nothing in the title or body we recognise"}
}

func firstMatch(haystack string, needles []string) string {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return n
		}
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return "1 known skill"
	}
	return itoa(n) + " known skills"
}

// itoa avoids pulling strconv in for one call in a package that is otherwise
// string handling only.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 && i > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
