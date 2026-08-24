package user

import "testing"

func itemByField(s Strength, field string) (StrengthItem, bool) {
	for _, i := range s.Items {
		if i.Field == field {
			return i, true
		}
	}
	return StrengthItem{}, false
}

// TestStrength_ZeroYearsIsAnAnswer is a regression test.
//
// A new graduate has zero years of experience. The completeness check read
// TotalYoE == 0 as "never answered" and told them to fill in a field they had
// already filled in — permanently, with no way to dismiss it.
//
// This is the same null-vs-zero rule the feed applies to undisclosed salary,
// and getting it wrong here punished exactly the users with the least to show.
func TestStrength_ZeroYearsIsAnAnswer(t *testing.T) {
	grad := Profile{
		FirstName: "Sam",
		Skills:    []string{"typescript", "react", "git", "sql", "javascript"},
		TotalYoE:  0,
		YoEStated: true,
	}

	item, ok := itemByField(grad.Strength(), "total_yoe")
	if !ok {
		t.Fatal("total_yoe item missing from the breakdown")
	}
	if !item.Done {
		t.Error("zero years was treated as unanswered — a new graduate has zero years")
	}

	never := grad
	never.YoEStated = false
	item, _ = itemByField(never.Strength(), "total_yoe")
	if item.Done {
		t.Error("a profile that never stated experience must not count as complete")
	}
}

// Every item states what it CHANGES about matching. A completeness meter whose
// rows say only "this is empty" is a nag; one that says what filling it in buys
// is a recommendation.
func TestStrength_EveryItemExplainsItself(t *testing.T) {
	s := Profile{}.Strength()
	if len(s.Items) == 0 {
		t.Fatal("no strength items")
	}
	for _, i := range s.Items {
		if i.Why == "" {
			t.Errorf("%s has no explanation of what it changes", i.Field)
		}
		if i.Label == "" {
			t.Errorf("%s has no label", i.Field)
		}
	}
}

func TestStrength_PercentTracksCompletion(t *testing.T) {
	empty := Profile{}.Strength()
	if empty.Percent != 0 {
		t.Errorf("empty profile = %d%%, want 0", empty.Percent)
	}

	full := Profile{
		Skills:    []string{"go", "sql", "aws", "docker", "linux"},
		TotalYoE:  5,
		YoEStated: true,
		Countries: []string{"IN"},
		Modes:     []string{"remote"},
		// A stated target title and a salary floor complete the set.
		TargetTitle: "Staff Engineer",
		CompMin:     5_000_000,
	}.Strength()
	if full.Percent != 100 {
		t.Errorf("complete profile = %d%%, want 100", full.Percent)
	}
}

// Fewer than five skills is not "has skills": the scorer weights skills at 40%,
// and a two-item list produces a coverage ratio that is noise.
func TestStrength_SkillsNeedsFive(t *testing.T) {
	thin := Profile{Skills: []string{"go", "sql"}}
	item, _ := itemByField(thin.Strength(), "skills")
	if item.Done {
		t.Error("two skills counted as complete; the bar is five")
	}
}
