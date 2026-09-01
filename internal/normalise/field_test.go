package normalise

import "testing"

// The left column is real: every title here was taken from the live corpus on
// 2026-09-01. A classifier tested only against titles someone invented is tested
// against the author's idea of the problem.
func TestClassifyField(t *testing.T) {
	cases := []struct {
		title  string
		skills int
		want   Field
	}{
		// Unambiguous software.
		{"Senior Backend Engineer", 8, FieldSoftware},
		{"Staff Software Engineer, Data Platform", 9, FieldSoftware},
		{"Engineering Manager - Database Change Management", 4, FieldSoftware},
		{"Senior Engineering Lead - Generative AI Platform", 11, FieldSoftware},
		{"AI Applications Engineer", 3, FieldSoftware},
		{"Programming Team Lead (Lead AI Behavior)", 2, FieldSoftware},

		// A negative title wins over a description full of tool names. Every one
		// of these carried three or more recognised skills in the corpus.
		{"Director, Strategic Finance & Investor Relations", 3, FieldOther},
		{"Deal Lead, Robinhood Ventures", 3, FieldOther},
		{"UT Technician", 0, FieldOther},
		{"Elektrotechniker / Elektromeister Konstruktion (m/w/d)", 0, FieldOther},
		{"Supply Chain Manager, Industrial Compute", 2, FieldOther},
		{"Technical Program Manager", 3, FieldOther},

		// "Engineering Manager" must survive the negative list. A bare "manager"
		// token would swallow the exact job this product is for, which is why
		// every negative phrase is specific.
		{"Engineering Manager (Data Platform)", 9, FieldSoftware},
		{"Strategic Sourcing Analyst", 3, FieldOther},
		{"Director, Travel & Expense", 3, FieldOther},
		{"Customer Success - Southern Europe", 3, FieldOther},
		{"Director, Lakebase Sales Specialists - Retail", 3, FieldOther},
		{"Senior Director, FSQA", 3, FieldOther},

		// "Engineer" is not enough on its own. Bosch posts these by the hundred.
		{"Asesor Comercial HVAC | Desarrollo de Negocios", 0, FieldOther},
		{"Mechanical Development Engineer", 1, FieldOther},
		{"Process Engineer - Manufacturing", 2, FieldOther},

		// Sales Engineer is the trap the ordering exists for: it contains
		// "engineer" and is not engineering.
		{"Sales Engineer", 7, FieldOther},

		// Honest abstention, which is most of the corpus.
		{"Area Sales Manager - Berlin", 0, FieldOther},
		{"Werkstudent People & Culture (d/w/m)", 0, FieldOther},
		// Still abstains, and should. Nothing in either title is a token we
		// recognise, and this is the cheap direction to be wrong in: the default
		// feed keeps `unknown`, so the reader sees one posting that does not fit
		// rather than losing one that did. See the note on Field.
		{"Engineer II", 1, FieldUnknown},
		{"Senior Associate", 0, FieldUnknown},
		{"Head of Product Creative", 0, FieldOther},
		{"", 9, FieldUnknown},
	}

	for _, c := range cases {
		got := ClassifyField(c.title, c.skills)
		if got.Field != c.want {
			t.Errorf("ClassifyField(%q, %d) = %s (%s), want %s",
				c.title, c.skills, got.Field, got.Because, c.want)
		}
	}
}

// Every classification must be able to say why. A label with no grounds is one a
// user can only distrust.
func TestClassifyField_AlwaysExplainsItself(t *testing.T) {
	for _, title := range []string{
		"Senior Backend Engineer", "Director, Travel & Expense",
		"Mechanical Development Engineer", "Senior Associate", "",
	} {
		if g := ClassifyField(title, 3); g.Because == "" {
			t.Errorf("ClassifyField(%q) gave no reason", title)
		}
	}
}

// Confidence has to mean something. A guess made from skills alone must not
// claim as much as one made from an unambiguous title.
func TestClassifyField_ConfidenceReflectsEvidence(t *testing.T) {
	strong := ClassifyField("Senior Backend Engineer", 0)
	weak := ClassifyField("Senior Associate", 9)

	if strong.Confidence <= weak.Confidence {
		t.Errorf("a title match (%.2f) must outrank a skills-only guess (%.2f)",
			strong.Confidence, weak.Confidence)
	}
	if weak.Field != FieldSoftware {
		t.Errorf("skills alone should still classify, got %s", weak.Field)
	}
	if weak.Confidence >= 0.5 {
		t.Errorf("a skills-only guess claimed %.2f — too much for the evidence",
			weak.Confidence)
	}
}
