// Package matching scores a posting against a user profile.
//
// The output is deliberately NOT a bare number. It is a band, a component
// breakdown, and a confidence — because a "87% match" implies a calibrated
// probability nobody in this industry actually has, and showing one to a user
// who has been marketed at by resume-optimisation vendors for two years is the
// fastest way to lose them.
//
// One rule runs through every component: **our uncertainty never becomes the
// user's penalty.** Where we could not extract something, the component returns
// neutral rather than a guess, so a posting is never pushed down a user's feed
// because of OUR parse failure.
package matching

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Band is the headline. Users see this; the number is available on expand.
type Band string

const (
	BandStrong    Band = "strong"
	BandPlausible Band = "plausible"
	BandStretch   Band = "stretch"
	BandUnlikely  Band = "unlikely"
)

// Profile is what we know about the user.
//
// Deliberately does NOT require a resume. Onboarding collects skills and years
// directly, which means scoring works from the first session — asking for a PDF
// upload before showing any value is the highest-friction possible first step.
type Profile struct {
	UserID    int64
	TotalYoE  float64
	Skills    map[string]float64 // canonical name -> years (0 if unstated)
	Countries []string
	Modes     []string
	CompMin   float64
	Currency  string
	// ParseConfidence is 1.0 for a self-declared profile and lower when the
	// data came from a resume we struggled to read.
	ParseConfidence float64
}

// Posting is the scoreable view of a job.
type Posting struct {
	ID               int64
	MustHaveSkills   []string
	NiceToHaveSkills []string
	YoEMin           *int16
	YoEMax           *int16
	YoEConfidence    float64
	Country          string
	Mode             string
	CompMin          *float64
	CompMax          *float64
	CompCurrency     string
	PostedAt         time.Time
	ParseConfidence  float64
}

// Component is one contribution, rendered directly in the UI as the explanation.
type Component struct {
	Name    string   `json:"name"`
	Score   float64  `json:"score"`
	Max     float64  `json:"max"`
	Detail  string   `json:"detail"`
	Matched []string `json:"matched,omitempty"`
	Missing []string `json:"missing,omitempty"`

	// Evidence is how much the source actually told us, from 0 (nothing) to 1
	// (enough to take the score at face value). Only the skills component sets
	// it today; it exists on Component because the caller must be able to ask
	// "how much of this is real" without knowing which component it holds.
	Evidence float64 `json:"evidence,omitempty"`
	// Neutral marks a component that abstained rather than scored. Shown
	// differently in the UI: "not enough information" is not the same as "bad".
	Neutral bool `json:"neutral"`
}

// Result is one user/posting score.
type Result struct {
	Score      float64     `json:"score"`
	Band       Band        `json:"band"`
	Components []Component `json:"components"`
	Confidence float64     `json:"confidence"`
	// Headline is one sentence a user can act on, e.g.
	// "7 of 9 must-haves. Missing: Kafka, Terraform."
	Headline string `json:"headline"`
}

// Weights are configuration, not constants.
//
// They are the part most likely to be wrong at launch and most likely to need
// tuning per market — an Indian SDE-1 search and a US staff search want
// different emphasis. Making them data means that is a config change with a
// version stamp on every score row, not a deploy.
type Weights struct {
	Skills       float64 `yaml:"skills"`
	Experience   float64 `yaml:"experience"`
	Location     float64 `yaml:"location"`
	Compensation float64 `yaml:"compensation"`
	Freshness    float64 `yaml:"freshness"`
}

// Config is a scoring profile.
type Config struct {
	Version string  `yaml:"version"`
	Weights Weights `yaml:"weights"`

	MustHaveRatio float64 `yaml:"must_have_ratio"`
	// AdjacentCredit is the fraction a related skill earns. Adjacency comes
	// from a CURATED table, never embeddings — embedding similarity places
	// "Java" and "JavaScript" next to each other, which is exactly the mistake
	// a candidate would find unforgivable.
	AdjacentCredit float64 `yaml:"adjacent_credit"`

	UnderYoEPenalty float64 `yaml:"under_yoe_penalty"`
	OverYoEPenalty  float64 `yaml:"over_yoe_penalty"`

	FreshnessHalfLifeDays float64 `yaml:"freshness_half_life_days"`

	BandStrong    float64 `yaml:"band_strong"`
	BandPlausible float64 `yaml:"band_plausible"`
	BandStretch   float64 `yaml:"band_stretch"`

	MinPostingParseConfidence float64 `yaml:"min_posting_parse_confidence"`

	// AbstentionCredit is the fraction of the skills weight granted when a
	// posting states no requirements we can read.
	//
	// This is NOT the midpoint of the scale, and the difference is the whole
	// point. "No information" is not "half marks" — it is whatever the readable
	// population actually earns, because that is the expected value of a
	// posting drawn at random from it. Crediting 0.5 when the population earns
	// 0.236 does not express ignorance, it expresses optimism, and it ranks
	// every unreadable posting above the majority of real ones.
	//
	// Re-measure with `make coverage` before changing it. See ADR-0011.
	AbstentionCredit float64 `yaml:"abstention_credit"`
}

// DefaultConfig is the launch profile. Every number here is a starting guess,
// which is precisely why it is configuration.
func DefaultConfig() Config {
	return Config{
		// Bumped whenever the algorithm changes in a way that would produce a
		// different number for the same inputs. Stored on every score row, and
		// the scheduler's stale-score sweep uses it to roll the change out
		// across the existing corpus. Forgetting to bump it means old scores
		// silently survive a fix — which is exactly what happened when the
		// evidence-weighting landed and a sales role kept its 98%.
		//
		// 2026.08.2: skills evidence weighting; unstated requirements credited
		// at the midpoint rather than dropped; band capped below strong when
		// evidence is partial.
		// 2026.08.3: year pluralisation in the experience reason string. Scores
		// are unchanged, but the stored `components` blob carries the text, so
		// the version bump is what makes the sweep re-render it.
		// 2026.08.4: skill vocabulary expanded from 47 to ~120 terms per ADR-0009.
		// Extraction reached nothing on 48.7% of live postings, which handed
		// freshness a disproportionate share of the score. Scores change, so the
		// stale sweep has to re-render every one of them.
		// 2026.08.5: merely-mentioned skills are used as nice-to-haves when a
		// posting has no parseable requirements section. 73.7% of extractions
		// landed as 'mentioned' and were being discarded, so the skills
		// component abstained on three postings in four even after the
		// vocabulary expansion.
		// 2026.08.7: the same calibrated credit now applies to PARTIAL evidence
		// too. It was hard-coded 0.5 there while the full-abstention path used
		// 0.25, so a posting stating one requirement scored better than one
		// stating none — one concept with two numbers.
		// 2026.08.6: the abstention credit is calibrated to the corpus rather
		// than to the midpoint of the scale. Measured: readable postings earn a
		// mean 0.236 of the skills weight; crediting unreadable ones at 0.5 put
		// their floor above the readable median and filled 42% of the top 100
		// with roles we could not read at all. Now 0.25. ADR-0011.
		Version: "2026.08.7",
		Weights: Weights{
			Skills:       40,
			Experience:   20,
			Location:     15,
			Compensation: 15,
			Freshness:    10,
		},
		MustHaveRatio:  0.75,
		AdjacentCredit: 0.5,

		// Asymmetric on purpose. Being UNDER the stated band is normal and
		// survivable — a large share of SDE-1 postings say "2+ years" and that
		// band is soft in practice, so self-filtering there costs real
		// opportunities. Being far OVER it means the role is a step backwards.
		UnderYoEPenalty: 4,
		OverYoEPenalty:  8,

		FreshnessHalfLifeDays: 7,

		BandStrong:    70,
		BandPlausible: 50,
		BandStretch:   30,

		MinPostingParseConfidence: 0.4,

		// Measured, not chosen: across 4,918 live postings the skills component
		// earned a mean of 0.236 and a median of 0.250 of its weight. Rounded to
		// the median — the third decimal is noise from one corpus on one day,
		// and carrying it would claim a precision the measurement does not have.
		AbstentionCredit: 0.25,
	}
}

// Adjacency maps a skill to related skills and the credit they earn.
type Adjacency map[string]map[string]float64

// DefaultAdjacency is hand-curated. Every pair is a deliberate judgement that a
// hiring manager would accept as transferable.
func DefaultAdjacency() Adjacency {
	pairs := map[string][]string{
		"postgresql": {"mysql", "sql"},
		"mysql":      {"postgresql", "sql"},
		"go":         {"rust", "java"},
		"java":       {"kotlin", "scala"},
		"kotlin":     {"java"},
		"python":     {"ruby"},
		"typescript": {"javascript"},
		"javascript": {"typescript"},
		"react":      {"vue", "svelte", "angular"},
		"vue":        {"react", "svelte"},
		"svelte":     {"react", "vue"},
		"kubernetes": {"docker"},
		"docker":     {"kubernetes"},
		"aws":        {"gcp", "azure"},
		"gcp":        {"aws", "azure"},
		"azure":      {"aws", "gcp"},
		"kafka":      {"rabbitmq"},
		"rabbitmq":   {"kafka"},
		"django":     {"flask", "rails"},
		"flask":      {"django"},
	}
	out := make(Adjacency, len(pairs))
	for skill, related := range pairs {
		m := make(map[string]float64, len(related))
		for _, r := range related {
			m[r] = 1.0 // scaled by Config.AdjacentCredit at scoring time
		}
		out[skill] = m
	}
	return out
}

// Scorer holds the configuration.
type Scorer struct {
	cfg Config
	adj Adjacency
}

func NewScorer(cfg Config, adj Adjacency) *Scorer {
	return &Scorer{cfg: cfg, adj: adj}
}

func (s *Scorer) Version() string { return s.cfg.Version }

// Score evaluates one posting against one profile.
func (s *Scorer) Score(p Profile, j Posting) Result {
	// Below the floor we understood too little of the posting to say anything.
	// Refusing to score is more honest than scoring fragments.
	if j.ParseConfidence < s.cfg.MinPostingParseConfidence {
		return Result{
			Score: 0, Band: BandUnlikely, Confidence: 0,
			Headline: "Not enough detail in this posting to score it",
			Components: []Component{{
				Name: "parse", Neutral: true,
				Detail: "We could not read enough of this posting to compare it fairly",
			}},
		}
	}

	components := []Component{
		s.scoreSkills(p, j),
		s.scoreExperience(p, j),
		s.scoreLocation(p, j),
		s.scoreCompensation(p, j),
		s.scoreFreshness(j),
	}

	// Neutral components are excluded from BOTH numerator and denominator, so
	// abstaining never drags the score down. A posting with no stated salary
	// scores exactly as if compensation were not a criterion at all.
	var earned, available float64
	// skillsEvidence is how much the posting told us about its requirements.
	// Zero when it listed none at all — the abstention path below.
	skillsEvidence := 0.0
	for _, c := range components {
		if c.Name == "skills" && !c.Neutral {
			skillsEvidence = c.Evidence
		}
		if c.Neutral {
			continue
		}
		earned += c.Score
		available += c.Max
	}

	// Skills abstention is NOT like the others, and treating it as though it
	// were produced the worst class of error this scorer can make.
	//
	// Observed in real data: "Professional Services Commercial Lead" scored 98
	// for a backend engineer. The posting listed no recognisable skills, so the
	// 40-point skills component abstained and vanished from the denominator,
	// leaving the score to be decided entirely by "you are remote, your years
	// are in range, it was posted today". Every one of those was true. The role
	// was not an engineering job at all.
	//
	// Salary is genuinely optional — a role is not a worse fit for failing to
	// publish one. Skills are the only component that establishes the posting is
	// even in the reader's field, so abstaining there means we do not know
	// whether this is a match, and the result must say so rather than quietly
	// reporting near-certainty about the fraction we could measure.
	//
	// The unknown weight is therefore credited rather than removed, which pulls
	// the score toward the population's own expectation in exact proportion to
	// how much we could not read.
	//
	// The credit was 0.5 — the midpoint of the SCALE — until it was measured
	// against the corpus. Readable postings earn a mean of 0.236, so half marks
	// for reading nothing put the FLOOR of the unreadable population (42.3)
	// above the MEDIAN of the readable one (39.2): a posting we understood
	// nothing about was guaranteed to outrank half the postings we understood.
	// 42 of the top 100 matches were roles we could not read. See ADR-0011.
	if skillsEvidence == 0 {
		skillsMax := s.cfg.Weights.Skills
		earned += skillsMax * s.cfg.AbstentionCredit
		available += skillsMax
	}

	score := 0.0
	if available > 0 {
		score = math.Round((earned/available)*1000) / 10
	}

	confidence := math.Min(p.ParseConfidence, j.ParseConfidence)

	band := s.bandFor(score)

	// A strong-match claim requires having actually read the requirements.
	//
	// Damping the skills component alone is not enough, because the other four
	// components can still carry a thin posting into the top band on their own:
	// a marketing role that is remote, in the right experience range and posted
	// today scores 81 with its skills already discounted. Every one of those
	// facts is true and none of them says the job is in the reader's field.
	//
	// So the band — the part a user reads as a claim rather than a number — is
	// capped whenever the evidence behind it is partial.
	if skillsEvidence < 1 && band == BandStrong {
		band = BandPlausible
	}
	if skillsEvidence < 1 {
		// Confidence degrades smoothly rather than by a fixed factor, so a
		// posting stating one of two expected skills is not treated the same as
		// one stating nothing at all.
		confidence *= 0.5 + 0.5*skillsEvidence
	}

	return Result{
		Score:      score,
		Band:       band,
		Components: components,
		Confidence: confidence,
		Headline:   headlineFor(components),
	}
}

func (s *Scorer) bandFor(score float64) Band {
	switch {
	case score >= s.cfg.BandStrong:
		return BandStrong
	case score >= s.cfg.BandPlausible:
		return BandPlausible
	case score >= s.cfg.BandStretch:
		return BandStretch
	default:
		return BandUnlikely
	}
}

// scoreSkills is the largest component and the one that decides whether scores
// are useful at all.
//
// The must-have / nice-to-have split is the whole ballgame: treating every
// technology mentioned as required makes every score uniformly low, and a
// uniformly low score cannot rank anything.
func (s *Scorer) scoreSkills(p Profile, j Posting) Component {
	c := Component{Name: "skills", Max: s.cfg.Weights.Skills}

	if len(j.MustHaveSkills) == 0 && len(j.NiceToHaveSkills) == 0 {
		c.Neutral = true
		c.Detail = "This posting does not list specific skills"
		return c
	}

	mustScore, mustMatched, mustMissing := s.coverage(p, j.MustHaveSkills)
	niceScore, niceMatched, _ := s.coverage(p, j.NiceToHaveSkills)

	ratio := s.cfg.MustHaveRatio
	switch {
	case len(j.MustHaveSkills) == 0:
		ratio = 0 // only nice-to-haves listed
	case len(j.NiceToHaveSkills) == 0:
		ratio = 1
	}

	coverageScore := ratio*mustScore + (1-ratio)*niceScore

	// Coverage is a RATIO, and a ratio over a tiny denominator is not evidence.
	//
	// Found in production: a "Marketing Strategy & Operations Manager" posting
	// listed exactly one preferred skill — SQL — which the reader happened to
	// have. Coverage was therefore 1.0, the skills component paid out its full
	// 40 points, and a marketing role landed at 98% for a backend engineer.
	// Matching the one thing a posting bothered to mention says almost nothing
	// about whether the job is a fit.
	//
	// So coverage is weighted by how much the posting actually specified, and
	// the unstated remainder is credited at the no-information level rather
	// than at whatever the sliver we could measure happened to say. Must-haves
	// count fully toward that evidence; nice-to-haves are softer signals and
	// count half.
	//
	// The unstated remainder is worth AbstentionCredit — the same quantity the
	// fully-abstaining path uses, because it is the same question: what is a
	// requirement we did not read worth? This was hard-coded 0.5 here while the
	// abstention path was calibrated to 0.25, so one concept had two numbers
	// and a posting stating one requirement was scored more generously than one
	// stating none. See ADR-0011.
	//
	// This also subsumes the no-skills case: zero stated skills gives zero
	// evidence and lands exactly on the credit, which is what the abstention
	// path already does.
	stated := float64(len(j.MustHaveSkills)) + 0.5*float64(len(j.NiceToHaveSkills))
	evidence := math.Min(1, stated/skillsFullEvidence)
	c.Evidence = evidence

	c.Score = c.Max * (coverageScore*evidence + s.cfg.AbstentionCredit*(1-evidence))
	c.Matched = append(mustMatched, niceMatched...)
	c.Missing = mustMissing

	switch {
	case len(j.MustHaveSkills) > 0:
		c.Detail = fmt.Sprintf("%d of %d must-haves", len(mustMatched), len(j.MustHaveSkills))
	default:
		c.Detail = fmt.Sprintf("%d of %d preferred skills", len(niceMatched), len(j.NiceToHaveSkills))
	}
	if evidence < 1 {
		c.Detail += " — this posting says little about what it needs"
	}
	return c
}

// skillsFullEvidence is how much stated requirement a posting needs before its
// skills coverage is taken at face value.
//
// Two must-haves is a low bar deliberately: most real engineering postings list
// far more, so this only damps the ones that named almost nothing — which are
// exactly the ones where a high coverage ratio is an artefact rather than a
// signal.
const skillsFullEvidence = 2

// coverage returns the fraction of required skills the user has, counting
// adjacent skills at partial credit.
func (s *Scorer) coverage(p Profile, required []string) (score float64, matched, missing []string) {
	if len(required) == 0 {
		return 1, nil, nil
	}

	var total float64
	for _, skill := range required {
		key := strings.ToLower(skill)
		if _, ok := p.Skills[key]; ok {
			total += 1
			matched = append(matched, skill)
			continue
		}

		// Adjacent credit: knowing MySQL is genuine evidence toward a
		// PostgreSQL requirement, and a hiring manager would agree.
		best := 0.0
		for related := range s.adj[key] {
			if _, has := p.Skills[related]; has {
				best = s.cfg.AdjacentCredit
				break
			}
		}
		if best > 0 {
			total += best
			matched = append(matched, skill+" (adjacent)")
		} else {
			missing = append(missing, skill)
		}
	}

	sort.Strings(matched)
	sort.Strings(missing)
	return total / float64(len(required)), matched, missing
}

// scoreExperience compares years, asymmetrically.
func (s *Scorer) scoreExperience(p Profile, j Posting) Component {
	c := Component{Name: "experience", Max: s.cfg.Weights.Experience}

	// A low-confidence YoE extraction is OUR failure, not the user's. Abstain
	// rather than penalise — otherwise a posting we misread gets pushed down
	// the feed of someone it might suit perfectly.
	if j.YoEConfidence < 0.5 || j.YoEMin == nil {
		c.Neutral = true
		c.Detail = "This posting does not state a clear experience requirement"
		return c
	}
	if p.TotalYoE <= 0 {
		c.Neutral = true
		c.Detail = "Add your years of experience to use this"
		return c
	}

	min := float64(*j.YoEMin)
	max := math.Inf(1)
	if j.YoEMax != nil {
		max = float64(*j.YoEMax)
	}

	under := math.Max(0, min-p.TotalYoE)
	over := 0.0
	if !math.IsInf(max, 1) {
		over = math.Max(0, p.TotalYoE-max)
	}

	penalty := under*s.cfg.UnderYoEPenalty + over*s.cfg.OverYoEPenalty
	c.Score = math.Max(0, c.Max-penalty)

	switch {
	case under > 0:
		// Framed as an encouragement, not a rejection. The "2+ years" band is
		// soft in practice and self-filtering there costs real opportunities.
		c.Detail = fmt.Sprintf("Asks for %s; you have %s — often still worth applying",
			plusYears(min), years(p.TotalYoE))
	case over > 0:
		c.Detail = fmt.Sprintf("Targets up to %s; you have %s", years(max), years(p.TotalYoE))
	default:
		c.Detail = "Your experience is in range"
	}
	return c
}

// years renders a year count as a person would say it.
//
// These strings are shown to the user verbatim, so "Targets up to 1 years" is a
// visible defect, not a cosmetic one — it is the product talking about their
// career in broken English. Fixed at the generator because the string is ours,
// not the vendor's, and every surface that renders it inherits the fix.
func years(n float64) string {
	if n == 1 {
		return "1 year"
	}
	return fmt.Sprintf("%.0f years", n)
}

// plusYears renders an open-ended lower bound: "5+ years", "1+ year".
func plusYears(n float64) string {
	if n == 1 {
		return "1+ year"
	}
	return fmt.Sprintf("%.0f+ years", n)
}

func (s *Scorer) scoreLocation(p Profile, j Posting) Component {
	c := Component{Name: "location", Max: s.cfg.Weights.Location}

	if len(p.Countries) == 0 && len(p.Modes) == 0 {
		c.Neutral = true
		c.Detail = "Set your location preferences to use this"
		return c
	}

	// Remote satisfies any geography, which is the whole point of remote.
	if j.Mode == "remote" && containsAny(p.Modes, "remote") {
		c.Score = c.Max
		c.Detail = "Remote — matches your preference"
		return c
	}

	countryOK := len(p.Countries) == 0 || j.Country == "" || containsAny(p.Countries, j.Country)
	modeOK := len(p.Modes) == 0 || j.Mode == "unknown" || containsAny(p.Modes, j.Mode)

	switch {
	case countryOK && modeOK:
		c.Score = c.Max
		c.Detail = "Location and work style match"
	case countryOK:
		// Right country, wrong arrangement — still worth surfacing.
		c.Score = c.Max * 0.5
		c.Detail = "Right country, different work arrangement"
	default:
		c.Score = 0
		c.Detail = "Outside your preferred locations"
	}
	return c
}

func (s *Scorer) scoreCompensation(p Profile, j Posting) Component {
	c := Component{Name: "compensation", Max: s.cfg.Weights.Compensation}

	// Undisclosed must NEVER be penalised. Only ~80% of postings publish
	// salary, and docking the rest would systematically down-rank every
	// Greenhouse and Lever posting — an artefact of the SOURCE, not the job.
	if j.CompMin == nil {
		c.Neutral = true
		c.Detail = "Salary not disclosed"
		return c
	}
	if p.CompMin <= 0 {
		c.Neutral = true
		c.Detail = "Set a target salary to use this"
		return c
	}
	// Comparing across currencies without live FX would be worse than
	// abstaining, so we abstain.
	if p.Currency != "" && j.CompCurrency != "" && p.Currency != j.CompCurrency {
		c.Neutral = true
		c.Detail = "Salary is in a different currency"
		return c
	}

	top := *j.CompMin
	if j.CompMax != nil {
		top = *j.CompMax
	}

	switch {
	case top >= p.CompMin*1.2:
		c.Score = c.Max
		c.Detail = "Comfortably above your target"
	case top >= p.CompMin:
		c.Score = c.Max * 0.8
		c.Detail = "Meets your target"
	case top >= p.CompMin*0.85:
		c.Score = c.Max * 0.4
		c.Detail = "Slightly below your target"
	default:
		c.Score = 0
		c.Detail = "Below your target"
	}
	return c
}

// scoreFreshness rewards recency.
//
// Included as a SCORED component rather than a ranking multiplier because
// timing is genuinely part of fit: a perfect match posted three weeks ago has
// several hundred applicants ahead of you, and pretending otherwise would be
// dishonest about the user's actual chances.
func (s *Scorer) scoreFreshness(j Posting) Component {
	c := Component{Name: "freshness", Max: s.cfg.Weights.Freshness}

	if j.PostedAt.IsZero() {
		c.Neutral = true
		c.Detail = "Posting date unknown"
		return c
	}

	days := time.Since(j.PostedAt).Hours() / 24
	if days < 0 {
		days = 0
	}
	decay := math.Pow(0.5, days/s.cfg.FreshnessHalfLifeDays)
	c.Score = c.Max * decay

	switch {
	case days <= 1:
		c.Detail = "Posted today — you are early"
	case days <= 3:
		c.Detail = fmt.Sprintf("Posted %.0f days ago", days)
	case days <= 14:
		c.Detail = fmt.Sprintf("Posted %.0f days ago — likely many applicants", days)
	default:
		c.Detail = "Posted over two weeks ago"
	}
	return c
}

// headlineFor builds one actionable sentence.
//
// Leads with what is missing, because that is the decision-relevant part: a
// user deciding whether to spend forty minutes needs the gap, not the praise.
func headlineFor(components []Component) string {
	for _, c := range components {
		if c.Name != "skills" || c.Neutral {
			continue
		}
		if len(c.Missing) > 0 {
			shown := c.Missing
			suffix := ""
			if len(shown) > 3 {
				shown, suffix = shown[:3], fmt.Sprintf(" +%d more", len(c.Missing)-3)
			}
			return fmt.Sprintf("%s. Missing: %s%s", c.Detail, strings.Join(shown, ", "), suffix)
		}
		return c.Detail + " — you have everything listed"
	}
	return "Scored on location, experience and recency"
}

func containsAny(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}
