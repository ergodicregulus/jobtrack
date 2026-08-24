package matching

import (
	"strings"
	"testing"
	"time"
)

// newScorer builds the production scorer. Tests run against the real
// configuration on purpose: a test that invents its own weights verifies
// arithmetic nobody ships.
func newScorer() *Scorer {
	return NewScorer(DefaultConfig(), DefaultAdjacency())
}

func backendProfile() Profile {
	return Profile{
		Skills: map[string]float64{
			"go": 5, "postgresql": 6, "kubernetes": 3, "kafka": 2, "terraform": 2,
		},
		TotalYoE:        7,
		Countries:       []string{"IN"},
		Modes:           []string{"remote"},
		ParseConfidence: 1,
	}
}

func livePosting() Posting {
	return Posting{
		MustHaveSkills:  []string{"go", "postgresql"},
		YoEMin:          ptr[int16](5),
		YoEMax:          ptr[int16](9),
		Mode:            "remote",
		Country:         "IN",
		PostedAt:        time.Now().Add(-24 * time.Hour),
		YoEConfidence:   1,
		ParseConfidence: 1,
	}
}

func ptr[T any](v T) *T { return &v }

func componentByName(r Result, name string) (Component, bool) {
	for _, c := range r.Components {
		if c.Name == name {
			return c, true
		}
	}
	return Component{}, false
}

// TestSkillsAbstentionCannotProduceStrongMatch is a regression test for a real
// defect found against live data.
//
// A posting with no extractable skills ("Professional Services Commercial
// Lead") scored 98 for a backend engineer, because the 40-point skills
// component abstained and was removed from the denominator, leaving location,
// years and freshness — all of which genuinely matched — to carry the whole
// score. The role was not an engineering job.
//
// The contract this locks in: when we cannot read what a posting requires, we
// do not get to claim near-certainty about it.
func TestSkillsAbstentionCannotProduceStrongMatch(t *testing.T) {
	s := newScorer()

	// Everything measurable is a perfect match; only the skills are unreadable.
	unreadable := Posting{
		MustHaveSkills:   nil,
		NiceToHaveSkills: nil,
		YoEMin:           ptr[int16](5),
		YoEMax:           ptr[int16](9),
		Mode:             "remote",
		Country:          "IN",
		PostedAt:         time.Now(),
		YoEConfidence:    1,
		ParseConfidence:  1,
	}

	got := s.Score(backendProfile(), unreadable)

	if got.Band == BandStrong {
		t.Errorf("band = %q, want anything but %q: a posting whose requirements "+
			"we could not read must not be presented as a confirmed strong match",
			got.Band, BandStrong)
	}

	// The score must be pulled toward the middle, not left near 100. With skills
	// (40) credited at its midpoint and everything else perfect, the ceiling is
	// (45 + 20) / 85 ~= 76.5.
	if got.Score > 80 {
		t.Errorf("score = %.1f, want <= 80: over half the weight was unmeasurable", got.Score)
	}

	skills, ok := componentByName(got, "skills")
	if !ok {
		t.Fatal("skills component missing from the breakdown")
	}
	if !skills.Neutral {
		t.Error("skills component should be marked neutral when nothing was listed")
	}

	// Confidence must reflect that half the picture is missing, so a caller can
	// filter on it rather than having to re-derive the same conclusion.
	if got.Confidence >= 1 {
		t.Errorf("confidence = %.2f, want < 1 when skills could not be read", got.Confidence)
	}
}

// A posting we CAN read, that genuinely matches, must still reach strong —
// otherwise the fix above would have solved the false positive by destroying
// every true positive with it.
func TestGenuineMatchStillScoresStrong(t *testing.T) {
	s := newScorer()

	got := s.Score(backendProfile(), livePosting())

	if got.Band != BandStrong {
		t.Errorf("band = %q, want %q (score %.1f, %s)",
			got.Band, BandStrong, got.Score, got.Headline)
	}
	if got.Score < 80 {
		t.Errorf("score = %.1f, want >= 80 for an exact skills and location match", got.Score)
	}
}

// Undisclosed salary must not be penalised. Roughly a fifth of postings do not
// publish one, and treating silence as "below your floor" would hide them.
func TestUndisclosedSalaryIsNotAPenalty(t *testing.T) {
	s := newScorer()
	p := backendProfile()
	p.CompMin = 6_000_000
	p.Currency = "INR"

	withSalary := livePosting()
	withSalary.CompMin = ptr(7_000_000.0)
	withSalary.CompCurrency = "INR"

	withoutSalary := livePosting()

	paid := s.Score(p, withSalary)
	silent := s.Score(p, withoutSalary)

	comp, ok := componentByName(silent, "compensation")
	if !ok {
		t.Fatal("compensation component missing")
	}
	if !comp.Neutral {
		t.Error("an undisclosed salary must abstain, not score zero")
	}

	// Both should land in the same band: abstaining on salary is not evidence
	// against a role.
	if paid.Band != silent.Band {
		t.Errorf("band changed on salary disclosure alone: with = %q (%.1f), without = %q (%.1f)",
			paid.Band, paid.Score, silent.Band, silent.Score)
	}
}

// Missing skills must be named. A score the reader cannot interrogate is a
// score they cannot overrule, and the whole design position is that we show
// our working.
func TestMissingSkillsAreReported(t *testing.T) {
	s := newScorer()

	j := livePosting()
	j.MustHaveSkills = []string{"go", "postgresql", "rust", "elixir"}

	got := s.Score(backendProfile(), j)

	skills, ok := componentByName(got, "skills")
	if !ok {
		t.Fatal("skills component missing")
	}

	missing := strings.Join(skills.Missing, ",")
	for _, want := range []string{"rust", "elixir"} {
		if !strings.Contains(missing, want) {
			t.Errorf("missing skills = %v, want it to include %q", skills.Missing, want)
		}
	}
	for _, notWant := range []string{"go", "postgresql"} {
		for _, m := range skills.Missing {
			if m == notWant {
				t.Errorf("%q is in the profile but was reported missing", notWant)
			}
		}
	}
}

// Asking for far more experience than the reader has should cost less than
// asking for far less. Being over-qualified is usually disqualifying in
// practice; being a year or two under frequently is not.
func TestExperiencePenaltyIsAsymmetric(t *testing.T) {
	s := newScorer()
	p := backendProfile() // 7 years

	under := livePosting()
	under.YoEMin, under.YoEMax = ptr[int16](11), ptr[int16](13) // 4 years above

	over := livePosting()
	over.YoEMin, over.YoEMax = ptr[int16](1), ptr[int16](3) // 4 years below

	underScore, _ := componentByName(s.Score(p, under), "experience")
	overScore, _ := componentByName(s.Score(p, over), "experience")

	if underScore.Score <= overScore.Score {
		t.Errorf("stretching upward (%.1f) should score no worse than being "+
			"over-qualified (%.1f)", underScore.Score, overScore.Score)
	}
}

// A posting we barely parsed must refuse to score rather than rank fragments.
func TestUnparseablePostingRefusesToScore(t *testing.T) {
	s := newScorer()

	j := livePosting()
	j.ParseConfidence = 0.1

	got := s.Score(backendProfile(), j)

	if got.Band != BandUnlikely || got.Score != 0 {
		t.Errorf("band = %q score = %.1f, want %q and 0 for an unreadable posting",
			got.Band, got.Score, BandUnlikely)
	}
	if got.Confidence != 0 {
		t.Errorf("confidence = %.2f, want 0", got.Confidence)
	}
}

// Adjacency is curated, never inferred. A related technology should earn
// partial credit; an unrelated one must earn none, or every score drifts upward
// until the ranking stops meaning anything.
func TestAdjacencyGivesPartialCreditOnly(t *testing.T) {
	s := newScorer()

	p := Profile{
		Skills:          map[string]float64{"postgresql": 5, "redis": 3},
		TotalYoE:        5,
		ParseConfidence: 1,
	}

	// Two must-haves so the posting clears skillsFullEvidence and the component
	// is taken at face value. With fewer, the low-evidence damping dominates and
	// this test would be measuring that instead of adjacency.
	exact := Posting{MustHaveSkills: []string{"postgresql", "redis"}, ParseConfidence: 1}
	adjacent := Posting{MustHaveSkills: []string{"mysql", "redis"}, ParseConfidence: 1}
	unrelated := Posting{MustHaveSkills: []string{"photoshop", "illustrator"}, ParseConfidence: 1}

	e, _ := componentByName(s.Score(p, exact), "skills")
	a, _ := componentByName(s.Score(p, adjacent), "skills")
	u, _ := componentByName(s.Score(p, unrelated), "skills")

	if !(e.Score > a.Score && a.Score > u.Score) {
		t.Errorf("expected exact (%.1f) > adjacent (%.1f) > unrelated (%.1f)",
			e.Score, a.Score, u.Score)
	}
	if u.Score != 0 {
		t.Errorf("unrelated skills scored %.1f, want 0 when the posting stated "+
			"enough for us to be sure", u.Score)
	}
}

// TestThinRequirementsAreNotEvidence is a regression test for a real defect.
//
// A "Marketing Strategy & Operations Manager" posting listed exactly one
// preferred skill — SQL — which a backend engineer happened to have. Coverage
// was 1.0, the 40-point skills component paid out in full, and the role scored
// 98% for that engineer.
//
// Matching the single thing a posting bothered to mention is not evidence that
// the job fits.
func TestThinRequirementsAreNotEvidence(t *testing.T) {
	s := newScorer()
	p := backendProfile()

	thin := livePosting()
	thin.MustHaveSkills = nil
	thin.NiceToHaveSkills = []string{"postgresql"} // the reader has it

	rich := livePosting()
	rich.MustHaveSkills = []string{"go", "postgresql", "kubernetes"}
	rich.NiceToHaveSkills = nil

	thinSkills, _ := componentByName(s.Score(p, thin), "skills")
	richSkills, _ := componentByName(s.Score(p, rich), "skills")

	// Both have perfect coverage; only one of them means anything.
	if thinSkills.Score >= richSkills.Score {
		t.Errorf("a single matched preferred skill scored %.1f against %.1f for a "+
			"fully-specified posting — coverage over a tiny denominator must not "+
			"pay out in full", thinSkills.Score, richSkills.Score)
	}

	thinResult := s.Score(p, thin)
	if thinResult.Band == BandStrong {
		t.Errorf("band = %q (score %.1f): a posting that stated one preferred "+
			"skill cannot support a strong-match claim", thinResult.Band, thinResult.Score)
	}
}

// Freshness must decay. Two identical postings a month apart cannot rank the
// same, or the product's central claim — that it surfaces roles early — is not
// reflected in its own ranking.
func TestFreshnessDecays(t *testing.T) {
	s := newScorer()
	p := backendProfile()

	today := livePosting()
	today.PostedAt = time.Now()

	old := livePosting()
	old.PostedAt = time.Now().Add(-45 * 24 * time.Hour)

	fresh, _ := componentByName(s.Score(p, today), "freshness")
	stale, _ := componentByName(s.Score(p, old), "freshness")

	if fresh.Score <= stale.Score {
		t.Errorf("fresh (%.2f) should score above stale (%.2f)", fresh.Score, stale.Score)
	}
}

// Generated reason strings are shown to the user verbatim, so a pluralisation
// slip is a visible defect rather than a cosmetic one — "Targets up to 1 years"
// is the product describing someone's career in broken English. Found in
// production data.
func TestExperienceDetail_ReadsAsEnglishAtBoundaries(t *testing.T) {
	s := newScorer()
	p := backendProfile() // 7 years

	cases := []struct {
		name           string
		min, max       int16
		wantSubstrings []string
		notWant        string
	}{
		{"upper bound of one year", 0, 1, []string{"up to 1 year;"}, "1 years"},
		{"upper bound of two years", 0, 2, []string{"up to 2 years"}, ""},
		{"lower bound of one year", 1, 1, []string{"up to 1 year;"}, "1 years"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := livePosting()
			j.YoEMin, j.YoEMax = &tc.min, &tc.max

			got, ok := componentByName(s.Score(p, j), "experience")
			if !ok {
				t.Fatal("experience component missing")
			}
			for _, want := range tc.wantSubstrings {
				if !strings.Contains(got.Detail, want) {
					t.Errorf("detail = %q, want it to contain %q", got.Detail, want)
				}
			}
			if tc.notWant != "" && strings.Contains(got.Detail, tc.notWant) {
				t.Errorf("detail = %q, must not contain %q", got.Detail, tc.notWant)
			}
		})
	}

	// A profile with exactly one year must also read correctly.
	one := backendProfile()
	one.TotalYoE = 1
	j := livePosting()
	lo, hi := int16(5), int16(9)
	j.YoEMin, j.YoEMax = &lo, &hi
	got, _ := componentByName(s.Score(one, j), "experience")
	if strings.Contains(got.Detail, "have 1 years") {
		t.Errorf("detail = %q, want \"1 year\" for a single year of experience", got.Detail)
	}
}

// TestIgnoranceDoesNotOutrankAMeasuredMatch is the second half of the
// abstention defect, and the half the first fix missed.
//
// Capping the BAND stopped an unreadable posting claiming "strong". It did not
// stop it outranking real ones, because the feed sorts on the number, not the
// band. Measured against 4,918 live postings: readable postings earned a mean
// 0.236 of the skills weight, while unreadable ones were credited 0.5 — so the
// FLOOR of the unreadable population (42.3) sat above the MEDIAN of the
// readable one (39.2), and 42 of the top 100 matches were roles we could not
// read at all.
//
// The contract: a posting we understood nothing about must rank below one we
// understood and found a genuine partial match in. Ignorance is not evidence.
func TestIgnoranceDoesNotOutrankAMeasuredMatch(t *testing.T) {
	s := newScorer()
	profile := backendProfile()

	// Identical in every respect a scorer can see — same location, same years,
	// same day — so the only thing separating them is what we could read.
	unreadable := livePosting()
	unreadable.MustHaveSkills = nil
	unreadable.NiceToHaveSkills = nil

	// A real engineering role the reader matches most of. Note what "most" has
	// to mean here: the credit is calibrated to the population's MEAN, so a
	// posting matching exactly the average fraction ties with a blank by
	// construction. That tie is the calibration working, not a bug — it is what
	// "no information" is worth. Only an above-average match should win.
	partial := livePosting()
	partial.MustHaveSkills = []string{"go", "postgresql", "rust", "elixir"}

	blank := s.Score(profile, unreadable)
	real := s.Score(profile, partial)

	if blank.Score >= real.Score {
		t.Fatalf("a posting we could not read (%.1f) outranked an above-average measured "+
			"match (%.1f); the abstention credit is above what readable postings earn",
			blank.Score, real.Score)
	}

	// And the credit must still be a credit, not a punishment: abstaining is
	// not evidence of a BAD match either, so an unreadable posting must not
	// sink below one we read and found nothing in.
	nothing := livePosting()
	nothing.MustHaveSkills = []string{"cobol", "fortran", "delphi", "perl"}
	none := s.Score(profile, nothing)

	if blank.Score <= none.Score {
		t.Fatalf("a posting we could not read (%.1f) ranked below one we read and matched "+
			"nothing in (%.1f); abstention must sit between the two, not at an extreme",
			blank.Score, none.Score)
	}
}

// TestAbstentionCreditIsCalibratedNotChosen guards the number itself.
//
// 0.5 is the tempting value and the wrong one: it is the midpoint of the SCALE,
// whereas the no-information expectation is what the POPULATION earns. Anyone
// "tidying" this back to a half will fail here and read why.
func TestAbstentionCreditIsCalibratedNotChosen(t *testing.T) {
	c := DefaultConfig()

	if c.AbstentionCredit >= 0.5 {
		t.Fatalf("AbstentionCredit is %.3f; at or above 0.5 it is the midpoint of the "+
			"scale rather than the measured expectation (0.236 mean, 0.250 median "+
			"across 4,918 live postings). Re-measure with `make coverage` before raising it.",
			c.AbstentionCredit)
	}
	if c.AbstentionCredit <= 0 {
		t.Fatalf("AbstentionCredit is %.3f; zero or less turns 'we could not read this' "+
			"into 'this is a bad match', which is a claim we have no evidence for.",
			c.AbstentionCredit)
	}
}
