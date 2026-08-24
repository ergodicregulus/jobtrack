package user

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// Profile is the queryable half of a user record.
//
// Split from Preferences on one rule: anything the matcher filters or sorts by
// lives in a column, anything only ever read back whole lives in the JSONB
// preferences blob. Years of experience decides which postings a user sees, so
// it is a column. Theme never appears in a WHERE clause, so it is not.
type Profile struct {
	FirstName    string   `json:"first_name"`
	LastName     string   `json:"last_name"`
	CurrentTitle string   `json:"current_title"`
	TargetTitle  string   `json:"target_title"`
	TotalYoE     float64  `json:"total_yoe"`
	Countries    []string `json:"pref_countries"`
	Modes        []string `json:"pref_modes"`
	CompMin      float64  `json:"pref_comp_min"`
	Currency     string   `json:"pref_currency"`
	// Skills is what the user declared by hand. The profile editor replaces
	// this set wholesale, which is why it must not contain anything inferred.
	Skills []string `json:"skills"`

	// ResumeSkills is what a CV contributed, kept separate and never included
	// in Skills.
	//
	// One list would be wrong in both directions: the editor PATCHes Skills
	// wholesale, so merging them would silently convert every inferred skill
	// into a declared one on the next save — and dropping them from the
	// response entirely (which is what happened first) made importing a CV
	// appear to do nothing at all, because the profile page rendered only
	// Skills and showed no change after fifteen were added.
	ResumeSkills []string `json:"resume_skills"`

	// SkillLabels maps a canonical name to how it should be WRITTEN.
	//
	// `Skills` stays canonical because it is the identifier the PATCH round
	// trips, and lower-casing is what makes matching reliable. But rendering
	// the identifier is how the profile ended up showing "postgresql",
	// "javascript" and "sql" as chips — the same defect that produced "Aws"
	// and "Graphql" on job cards, reached from the other direction. One map,
	// covering exactly the skills this user has, so the client never has to
	// invent a casing rule of its own.
	SkillLabels map[string]string `json:"skill_labels"`

	Onboarded bool `json:"onboarded"`

	// YoEStated distinguishes "zero years" from "never answered".
	//
	// TotalYoE is a plain float64 for the API's convenience, and 0 is a
	// perfectly ordinary answer — a new graduate has zero years. Without this
	// flag the completeness check read 0 as "unset" and told every new graduate
	// to fill in a field they had already filled in, forever. Same null-vs-zero
	// rule the feed applies to undisclosed salary.
	YoEStated bool `json:"yoe_stated"`
}

// DisplayName is what the UI greets the user with.
//
// Falls back through full name, first name, then empty — never to the email
// local-part. Greeting someone as "n.thalluri06" reads like a system message,
// not a welcome, and the whole point of asking for a name during onboarding is
// to avoid exactly that.
func (p Profile) DisplayName() string {
	switch {
	case p.FirstName != "" && p.LastName != "":
		return p.FirstName + " " + p.LastName
	case p.FirstName != "":
		return p.FirstName
	default:
		return ""
	}
}

// Initials backs the avatar. Two letters at most.
func (p Profile) Initials() string {
	first := firstRune(p.FirstName)
	last := firstRune(p.LastName)
	if first == "" {
		return "?"
	}
	return first + last
}

func firstRune(s string) string {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return strings.ToUpper(string(r))
		}
	}
	return ""
}

// Work modes and their validation set. Mirrors the work_mode enum in the
// schema; a value that passes here but not there would surface as a 500 from a
// cast failure rather than a 400, which is the wrong error for bad input.
var validModes = map[string]bool{"remote": true, "hybrid": true, "onsite": true}

// Currencies the compensation normaliser can compare across. Anything else is
// stored but not used for filtering, so accepting it would silently drop the
// user's minimum-salary filter.
var validCurrencies = map[string]bool{"INR": true, "USD": true, "EUR": true, "GBP": true, "CAD": true, "AUD": true, "SGD": true}

// ProfileUpdate is a partial update: nil means "leave alone", which is what
// makes the onboarding wizard able to save each step independently without the
// later steps clobbering the earlier ones with zero values.
type ProfileUpdate struct {
	FirstName    *string   `json:"first_name,omitempty"`
	LastName     *string   `json:"last_name,omitempty"`
	CurrentTitle *string   `json:"current_title,omitempty"`
	TargetTitle  *string   `json:"target_title,omitempty"`
	TotalYoE     *float64  `json:"total_yoe,omitempty"`
	Countries    *[]string `json:"pref_countries,omitempty"`
	Modes        *[]string `json:"pref_modes,omitempty"`
	CompMin      *float64  `json:"pref_comp_min,omitempty"`
	Currency     *string   `json:"pref_currency,omitempty"`
	Skills       *[]string `json:"skills,omitempty"`
}

const (
	maxNameLen  = 80
	maxTitleLen = 120
	maxSkills   = 60
	maxYoE      = 60 // a 60-year career is already implausible; 100 is a typo
)

// Apply validates and merges an update.
//
// Validation is here rather than in the handler so the same rules apply to
// onboarding, the profile page and any future import path. A rule enforced in
// one handler is a rule that will be missing from the second one.
func (p *Profile) Apply(u ProfileUpdate) error {
	if u.FirstName != nil {
		v := strings.TrimSpace(*u.FirstName)
		if err := checkLen("first_name", v, maxNameLen); err != nil {
			return err
		}
		p.FirstName = v
	}
	if u.LastName != nil {
		v := strings.TrimSpace(*u.LastName)
		if err := checkLen("last_name", v, maxNameLen); err != nil {
			return err
		}
		p.LastName = v
	}
	if u.CurrentTitle != nil {
		v := strings.TrimSpace(*u.CurrentTitle)
		if err := checkLen("current_title", v, maxTitleLen); err != nil {
			return err
		}
		p.CurrentTitle = v
	}
	if u.TargetTitle != nil {
		v := strings.TrimSpace(*u.TargetTitle)
		if err := checkLen("target_title", v, maxTitleLen); err != nil {
			return err
		}
		p.TargetTitle = v
	}
	if u.TotalYoE != nil {
		v := *u.TotalYoE
		if v < 0 || v > maxYoE {
			return fmt.Errorf("total_yoe must be between 0 and %d", maxYoE)
		}
		p.TotalYoE = v
	}
	if u.Countries != nil {
		out := make([]string, 0, len(*u.Countries))
		for _, c := range *u.Countries {
			c = strings.ToUpper(strings.TrimSpace(c))
			if len(c) != 2 {
				return fmt.Errorf("pref_countries must be ISO 3166-1 alpha-2 codes, got %q", c)
			}
			out = append(out, c)
		}
		p.Countries = dedupe(out)
	}
	if u.Modes != nil {
		out := make([]string, 0, len(*u.Modes))
		for _, m := range *u.Modes {
			m = strings.ToLower(strings.TrimSpace(m))
			if !validModes[m] {
				return fmt.Errorf("pref_modes must be remote, hybrid or onsite, got %q", m)
			}
			out = append(out, m)
		}
		p.Modes = dedupe(out)
	}
	if u.CompMin != nil {
		if *u.CompMin < 0 {
			return fmt.Errorf("pref_comp_min cannot be negative")
		}
		p.CompMin = *u.CompMin
	}
	if u.Currency != nil {
		v := strings.ToUpper(strings.TrimSpace(*u.Currency))
		if v != "" && !validCurrencies[v] {
			return fmt.Errorf("pref_currency %q is not supported", v)
		}
		p.Currency = v
	}
	if u.Skills != nil {
		if len(*u.Skills) > maxSkills {
			return fmt.Errorf("at most %d skills", maxSkills)
		}
		out := make([]string, 0, len(*u.Skills))
		for _, s := range *u.Skills {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "" {
				continue
			}
			if len(s) > 64 {
				return fmt.Errorf("skill %q is too long", s)
			}
			out = append(out, s)
		}
		p.Skills = dedupe(out)
	}
	return nil
}

// CanCompleteOnboarding reports whether enough is known to produce useful
// matches, and says what is missing when it is not.
//
// The bar is deliberately low. Only the fields the scorer cannot work without
// are required: a name (so the product can address the user), and years of
// experience plus at least one skill (without which every score would be a
// coin flip). Everything else improves matches but does not gate entry —
// forcing a complete profile before showing a single job inverts the value
// exchange onboarding is supposed to make.
func (p Profile) CanCompleteOnboarding() (bool, []string) {
	var missing []string
	if p.FirstName == "" {
		missing = append(missing, "first_name")
	}
	if len(p.Skills) == 0 {
		missing = append(missing, "skills")
	}
	if len(missing) > 0 {
		return false, missing
	}
	return true, nil
}

// Strength reports how much of the profile is filled in, and what filling in
// the rest would buy.
//
// Not a vanity meter. Each item names the scoring component it feeds, because
// "your profile is 60% complete" tells someone nothing about whether to care.
// The weights mirror the scorer: skills dominate, so the skills prompt is worth
// the most and appears first when missing.
type Strength struct {
	Percent int            `json:"percent"`
	Items   []StrengthItem `json:"items"`
}

type StrengthItem struct {
	Field string `json:"field"`
	Label string `json:"label"`
	Done  bool   `json:"done"`
	// Why states what this field changes about matching, in the user's terms.
	Why string `json:"why"`
}

func (p Profile) Strength() Strength {
	items := []StrengthItem{
		{
			Field: "skills", Label: "List at least five skills",
			Done: len(p.Skills) >= 5,
			Why:  "Skills are 40% of every match score — the largest single component",
		},
		{
			Field: "total_yoe", Label: "Set your years of experience",
			Done: p.YoEStated,
			Why:  "Decides which roles are a stretch and which are a step back",
		},
		{
			Field: "pref_countries", Label: "Choose where you would work",
			Done: len(p.Countries) > 0,
			Why:  "Without it, roles you cannot take are scored as though you could",
		},
		{
			Field: "pref_modes", Label: "Choose remote, hybrid or on-site",
			Done: len(p.Modes) > 0,
			Why:  "Filters out the arrangements you would turn down",
		},
		{
			Field: "target_title", Label: "Name the role you want next",
			Done: p.TargetTitle != "",
			Why:  "Used to rank adjacent titles you might not have searched for",
		},
		{
			Field: "pref_comp_min", Label: "Set a minimum salary",
			Done: p.CompMin > 0,
			Why:  "Roles below it drop down the list; undisclosed ones are still shown",
		},
	}

	done := 0
	for _, it := range items {
		if it.Done {
			done++
		}
	}
	return Strength{
		Percent: int(math.Round(float64(done) / float64(len(items)) * 100)),
		Items:   items,
	}
}

func checkLen(field, v string, max int) error {
	if len([]rune(v)) > max {
		return fmt.Errorf("%s must be %d characters or fewer", field, max)
	}
	return nil
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
