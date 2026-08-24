package normalise

import (
	"regexp"
	"sort"
	"strings"
)

// Requirement mirrors the skill_requirement SQL enum.
type Requirement string

const (
	MustHave   Requirement = "must_have"
	NiceToHave Requirement = "nice_to_have"
	Mentioned  Requirement = "mentioned"
)

// ExtractedSkill is one skill found in a description.
type ExtractedSkill struct {
	Canonical   string
	Requirement Requirement
	Confidence  Confidence
}

// Section headings that classify everything beneath them.
//
// This split is the single largest driver of score quality. Treating every
// technology mentioned as required produces uniformly low scores — every
// posting names fifteen technologies and nobody has all fifteen — and a
// uniformly low score cannot rank anything.
var (
	mustHaveHeading = regexp.MustCompile(`(?i)^\s*(?:what you|you)?\s*(requirements?|qualifications?|must[- ]haves?|you have|basic qualifications?|minimum qualifications?|we're looking for|what we require|skills? (?:required|needed))\b`)
	niceHeading     = regexp.MustCompile(`(?i)^\s*(nice[- ]to[- ]haves?|bonus(?: points)?|preferred(?: qualifications?)?|pluses?|desirable|good to have|it'?s a plus|extra credit)\b`)
	neutralHeading  = regexp.MustCompile(`(?i)^\s*(about (?:us|the (?:role|team|company))|benefits?|perks?|what we offer|compensation|our stack|responsibilities|what you'?ll do|the role|why join)\b`)
)

// Vocabulary is the canonical skill list with aliases.
//
// Held in memory: it is a few thousand entries, read on every posting, and
// changes weekly at most. A database round trip per skill per posting would be
// the definition of an N+1.
type Vocabulary struct {
	// byAlias maps every lowercase alias to its canonical name.
	byAlias map[string]string
	// patterns holds pre-compiled word-boundary matchers, built once.
	patterns map[string]*regexp.Regexp
}

// Canonicals returns every canonical skill name, sorted.
//
// This exists so the vocabulary is the SINGLE source of truth for the skill
// list. It was not, and the consequence was silent: `posting_skills` is written
// with an INNER JOIN onto the `skills` table, so any skill the extractor found
// but the table did not contain was discarded without a word. Expanding the
// vocabulary from 47 to ~120 terms therefore changed nothing at all until the
// two lists were reconciled.
func (v *Vocabulary) Canonicals() []string {
	seen := make(map[string]struct{}, len(v.byAlias))
	out := make([]string, 0, len(v.byAlias))
	for _, canonical := range v.byAlias {
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	sort.Strings(out)
	return out
}

// NewVocabulary builds the lookup from canonical -> aliases.
func NewVocabulary(entries map[string][]string) *Vocabulary {
	v := &Vocabulary{
		byAlias:  make(map[string]string, len(entries)*3),
		patterns: make(map[string]*regexp.Regexp, len(entries)*3),
	}
	for canonical, aliases := range entries {
		all := append([]string{canonical}, aliases...)
		for _, a := range all {
			la := strings.ToLower(a)
			v.byAlias[la] = canonical
			v.patterns[la] = compileSkillPattern(la)
		}
	}
	return v
}

// compileSkillPattern builds a word-boundary matcher.
//
// Go's \b does not treat '+' or '#' as word characters, so a naive \bc\+\+\b
// never matches "C++". These are exactly the skills people care most about
// getting right, so they get explicit handling.
func compileSkillPattern(alias string) *regexp.Regexp {
	quoted := regexp.QuoteMeta(alias)
	// Leading boundary: start, or a non-alphanumeric character.
	// Trailing boundary: end, or a character that cannot continue the token.
	return regexp.MustCompile(`(?i)(?:^|[^a-z0-9+#.])` + quoted + `(?:$|[^a-z0-9+#])`)
}

// ambiguousPatterns supply CORROBORATING evidence for skill names that are also
// ordinary English words.
//
// Word boundaries solve the substring problem ("go" inside "going") and do
// nothing at all for the harder one: a standalone word used in its English
// sense. Found in production — an "Account Executive" posting was scored a 96%
// match for a backend engineer because "go to market" registered as the Go
// programming language, and that single false skill was 40% of the score.
//
// A bare occurrence of one of these is accepted only when the surrounding text
// makes it a skill rather than a word. Two independent signals qualify:
//
//  1. An unambiguous phrasing anywhere — "golang", "go developer", "written in
//     go", "go 1.22". These patterns.
//  2. A short line inside a requirements or nice-to-have section. "Strong Go"
//     as a bullet is the language; "you will go above and beyond for customers"
//     is not, and length separates them reliably because skill bullets are
//     terse and prose is not. See ambiguousBareLimit.
//
// This trades a little recall for precision deliberately. Missing a Go role
// that only ever writes "Go" in a long sentence costs one listing; calling a
// sales job a strong match destroys trust in every score on the page.
// ambiguousBareLimit is the word count below which a line under a requirements
// heading is treated as a skill bullet rather than a sentence. Six covers
// "Strong Go", "Go and Kubernetes", "3+ years of Go" while excluding the prose
// that produces the false positives.
const ambiguousBareLimit = 6

// ambiguousExclusions veto a match even when the corroborating pattern fired.
//
// Each entry is a different sense of the same word that is common enough in job
// postings to matter — overwhelmingly in the industries that also hire
// engineers, which is exactly why they collide.
var ambiguousExclusions = map[string]*regexp.Regexp{
	// The interbank messaging network. Ubiquitous in fintech postings.
	"swift": regexp.MustCompile(`(?i)\bswift\s*(?:payments?|network|transfers?|codes?|bic|messaging|rails|gpi|mt\d)`),
	// Corrosion, and the American industrial region.
	"rust": regexp.MustCompile(`(?i)\brust\s*(?:belt|proof|resistant|free)`),
	// The season and the mechanical part.
	"spring": regexp.MustCompile(`(?i)\bspring\s*(?:20\d\d|semester|term|break|season|intake)`),
	// Corporate-values prose.
	"spark": regexp.MustCompile(`(?i)\bspark\s+(?:joy|innovation|curiosity|creativity|ideas|conversations?|change)`),
	// Employment security, not the discipline.
	"security": regexp.MustCompile(`(?i)\b(?:job|employment|financial|social)\s+security\b`),
}

var ambiguousPatterns = map[string]*regexp.Regexp{
	"go": regexp.MustCompile(`(?i)(?:` +
		`\bgolang\b` +
		`|(?:\b(?:in|with|using|written in|experience in|knowledge of|proficient in)\s+)go\b` +
		`|\bgo\s+(?:developer|engineer|programming|language|services?|routines?|modules?|code|codebase|backend|microservices|api)\b` +
		`|\bgo\s*1\.\d` +
		`)`),
	"r": regexp.MustCompile(`(?i)(?:` +
		`\br\s+(?:programming|language|studio|shiny)\b` +
		`|(?:\b(?:in|with|using)\s+)r\b(?:\s*[,/)]|\s+and\b)` +
		`)`),
	"c": regexp.MustCompile(`(?i)(?:` +
		`\bc\s+(?:programming|language|developer|engineer)\b` +
		`|\bc/c\+\+` +
		`|(?:\b(?:in|with|using)\s+)c\b(?:\s*[,/)]|\s+and\b)` +
		`)`),
	// "Swift" is also a payments network, and every fintech posting mentions it.
	"swift": regexp.MustCompile(`(?i)(?:` +
		`\bswiftui\b` +
		`|\bswift\s+(?:developer|engineer|programming|language|code)\b` +
		`|(?:\b(?:in|with|using)\s+)swift\b` +
		`|\bios\b[^.]{0,40}\bswift\b` +
		`|\bswift\b[^.]{0,40}\bios\b` +
		`)`),
	// "Spark" is a verb in nearly every company-values paragraph ever written:
	// "spark joy", "spark innovation", "spark curiosity".
	"spark": regexp.MustCompile(`(?i)(?:` +
		`\bapache\s+spark\b` +
		`|\bpyspark\b` +
		`|\bspark\s+(?:streaming|sql|jobs?|clusters?|developer|engineer)\b` +
		`|(?:\b(?:in|with|using)\s+)spark\b` +
		`)`),
	// "At the helm" is the common English use.
	"helm": regexp.MustCompile(`(?i)(?:` +
		`\bhelm\s+(?:charts?|releases?)\b` +
		`|\bkubernetes\b[^.]{0,40}\bhelm\b` +
		`|(?:\b(?:in|with|using)\s+)helm\b` +
		`)`),
	// "Security" appears in prose constantly — "security of tenure", "job
	// security", "we take security seriously". As a SKILL it is qualified.
	"security": regexp.MustCompile(`(?i)(?:` +
		`\b(?:app|application|cloud|network|product|infrastructure)\s+security\b` +
		`|\bsecurity\s+(?:engineer|engineering|review|audits?|best practices)\b` +
		`|\bappsec\b` +
		`)`),
	// "Spring" is a season and a verb far more often than it is a framework.
	"spring": regexp.MustCompile(`(?i)(?:` +
		`\bspring\s*boot\b` +
		`|\bspring\s+(?:framework|mvc|cloud|security|data)\b` +
		`|\bjava\b[^.]{0,40}\bspring\b` +
		`)`),
	// "Rust" is corrosion, and "rust-proof"/"rust belt" appear in prose.
	"rust": regexp.MustCompile(`(?i)(?:` +
		`\brust\s+(?:developer|engineer|programming|language|code|crates?)\b` +
		`|(?:\b(?:in|with|using|written in)\s+)rust\b` +
		`|\brustlang\b` +
		`|\bcargo\b[^.]{0,40}\brust\b` +
		`)`),
}

// DefaultVocabulary is the seed skill set. Deliberately small and curated: a
// scraped list produces false positives like matching "go" inside "going".
func DefaultVocabulary() *Vocabulary {
	return NewVocabulary(map[string][]string{
		"go":         {"golang"},
		"python":     {"py", "python3"},
		"java":       {},
		"javascript": {"js", "ecmascript"},
		"typescript": {"ts"},
		"rust":       {},
		"c++":        {"cpp"},
		"c#":         {"csharp", ".net"},
		"ruby":       {},
		"php":        {},
		"kotlin":     {},
		"swift":      {},
		"scala":      {},

		"postgresql":    {"postgres", "psql", "pgsql"},
		"mysql":         {"mariadb"},
		"mongodb":       {"mongo"},
		"redis":         {},
		"elasticsearch": {"elastic search", "opensearch"},
		"cassandra":     {},
		"dynamodb":      {},
		"clickhouse":    {},

		"django":  {},
		"flask":   {"fastapi"},
		"rails":   {"ruby on rails"},
		"spring":  {"spring boot"},
		"react":   {"reactjs", "react.js"},
		"vue":     {"vuejs", "vue.js"},
		"svelte":  {"sveltekit"},
		"angular": {},
		"node.js": {"nodejs", "node"},

		"kubernetes": {"k8s"},
		"docker":     {"containers"},
		"terraform":  {},
		"aws":        {"amazon web services"},
		"gcp":        {"google cloud"},
		"azure":      {},
		"kafka":      {"apache kafka"},
		"rabbitmq":   {},
		"grpc":       {},
		"graphql":    {},

		"ci/cd":         {"cicd", "continuous integration", "github actions", "gitlab ci", "jenkins", "circleci"},
		"observability": {"opentelemetry", "otel", "prometheus", "grafana", "datadog", "splunk", "sentry"},
		"microservices": {},
		"rest":          {"restful", "rest api"},
		"sql":           {},
		"linux":         {"unix"},
		"git":           {},

		// --- Added 2026-08-16 per ADR-0009 ---
		//
		// The vocabulary held 47 terms and extracted NOTHING from 48.7% of live
		// postings. Because an abstaining skills component removes 40 of 100
		// points from the denominator, that handed freshness a disproportionate
		// share of the score — the ranking correlated more strongly with a
		// posting's age (0.256) than with whether the reader could do the job
		// (0.173).
		//
		// Everything below is added WITH its aliases, and anything that collides
		// with ordinary English gets a context rule in ambiguousPatterns and a
		// test. A scraped taxonomy would undo the precision work that made
		// "go to market" stop registering as Golang.

		// Languages the original list missed entirely.
		"c":           {}, // ambiguous — see ambiguousPatterns
		"r":           {}, // ambiguous — see ambiguousPatterns
		"perl":        {},
		"elixir":      {"phoenix"},
		"erlang":      {},
		"haskell":     {},
		"clojure":     {},
		"dart":        {"flutter"},
		"objective-c": {"objc"},
		"bash":        {"shell scripting", "shell script"},
		"powershell":  {},

		// Data and analytics — a large share of engineering postings, and the
		// original vocabulary covered almost none of it.
		"spark":      {"apache spark", "pyspark"},
		"airflow":    {"apache airflow"},
		"dbt":        {},
		"snowflake":  {},
		"databricks": {},
		"bigquery":   {},
		"redshift":   {},
		"hadoop":     {},
		"flink":      {"apache flink"},
		"pandas":     {},
		"numpy":      {},
		"etl":        {"data pipelines", "data pipeline"},

		// Machine learning. Absent entirely, in a market where a growing share of
		// engineering roles mention it.
		"pytorch":      {}, // NOT "torch" — too common as an English word
		"tensorflow":   {}, // NOT "tf" — that is Terraform shorthand far more often
		"scikit-learn": {"sklearn"},
		"llm":          {"large language model", "large language models", "genai", "generative ai"},
		"nlp":          {"natural language processing"},
		"mlops":        {},

		// Infrastructure and platform.
		"ansible":       {},
		"pulumi":        {},
		"helm":          {},
		"nginx":         {},
		"envoy":         {},
		"istio":         {"service mesh"},
		"kafka streams": {},
		// NOT bare "lambda" — it is an anonymous function at least as often.
		"serverless": {"aws lambda", "cloud functions"},
		"cdn":        {"cloudflare", "cloudfront"},

		// Datastores the original list missed.
		"sqlite":    {},
		"neo4j":     {"graph database"},
		"memcached": {},
		"kinesis":   {},
		"sqs":       {},
		"s3":        {"object storage"},

		// Frontend beyond the four frameworks already listed.
		"next.js":       {"nextjs"},
		"tailwind":      {"tailwindcss"},
		"webpack":       {},
		"vite":          {},
		"css":           {},
		"html":          {},
		"accessibility": {"wcag", "a11y"},

		// Practices and testing — frequently the only "skills" a posting names.
		"testing":             {"unit testing", "integration testing", "test automation"},
		"tdd":                 {"test driven development"},
		"code review":         {},
		"agile":               {"scrum", "kanban"},
		"distributed systems": {},
		"system design":       {},
		"api design":          {},
		"security":            {"appsec", "application security"},
		"performance":         {"performance tuning", "profiling"},
		"caching":             {},
		"monitoring":          {"alerting", "on-call", "oncall"},
		"scalability":         {},
		"websockets":          {"websocket"},
		"oauth":               {"oidc", "sso"},
		"protobuf":            {"protocol buffers"},
	})
}

// ExtractSkills finds skills and classifies them by the section they appear in.
func (v *Vocabulary) ExtractSkills(descriptionText string) []ExtractedSkill {
	if descriptionText == "" {
		return nil
	}

	// Track the strongest classification per skill: a skill named in both
	// Requirements and Nice-to-have is a must-have.
	best := make(map[string]ExtractedSkill)
	current := Mentioned

	for _, line := range strings.Split(descriptionText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// A heading changes the section for everything that follows.
		switch {
		case mustHaveHeading.MatchString(trimmed):
			current = MustHave
			continue
		case niceHeading.MatchString(trimmed):
			current = NiceToHave
			continue
		case neutralHeading.MatchString(trimmed):
			current = Mentioned
			continue
		}

		lower := strings.ToLower(trimmed)
		// A terse line under a requirements heading reads as a skill list, so a
		// bare ambiguous token is credible there. Anywhere else it needs the
		// corroborating phrasing below.
		terseRequirement := (current == MustHave || current == NiceToHave) &&
			len(strings.Fields(lower)) <= ambiguousBareLimit

		for alias, canonical := range v.byAlias {
			if !v.patterns[alias].MatchString(lower) {
				continue
			}
			if ctx, ambiguous := ambiguousPatterns[canonical]; ambiguous {
				if !terseRequirement && !ctx.MatchString(lower) {
					continue
				}
				// Corroboration can itself be a false positive: "with SWIFT
				// payment rails" satisfies the "preposition of use" rule while
				// meaning the banking network. An explicit veto is needed
				// because RE2 has no lookahead to express it inside the pattern.
				if veto, ok := ambiguousExclusions[canonical]; ok && veto.MatchString(lower) {
					continue
				}
			}
			conf := ConfidenceStrong
			if current == Mentioned {
				// Outside a requirements section this is a passing reference —
				// "our stack includes X" is not a requirement.
				conf = ConfidenceWeak
			}
			candidate := ExtractedSkill{
				Canonical: canonical, Requirement: current, Confidence: conf,
			}
			if existing, ok := best[canonical]; !ok || stronger(candidate, existing) {
				best[canonical] = candidate
			}
		}
	}

	out := make([]ExtractedSkill, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	return out
}

// stronger ranks must_have > nice_to_have > mentioned.
func stronger(a, b ExtractedSkill) bool {
	return rank(a.Requirement) > rank(b.Requirement)
}

func rank(r Requirement) int {
	switch r {
	case MustHave:
		return 3
	case NiceToHave:
		return 2
	default:
		return 1
	}
}

// displayNames overrides the default title-casing for names that have a
// conventional written form.
//
// Display names were generated with SQL `initcap()`, which produced "Aws",
// "Graphql", "Llm", "Node.Js", "Postgresql" and "Ci/Cd" — visible on every job
// card, every chip and every gap line in the product. Getting a technology's
// name wrong on screen is a small thing that reads as not knowing the field,
// to an audience that works in it.
//
// Only entries that differ from title-casing are listed; anything absent falls
// through to it, which is correct for "Docker", "Kubernetes", "Terraform".
var displayNames = map[string]string{
	"aws": "AWS", "gcp": "GCP", "sql": "SQL", "nosql": "NoSQL",
	"html": "HTML", "css": "CSS", "http": "HTTP", "json": "JSON",
	"yaml": "YAML", "xml": "XML", "jwt": "JWT", "oauth": "OAuth",
	"rest": "REST", "grpc": "gRPC", "graphql": "GraphQL", "api": "API",
	"llm": "LLM", "nlp": "NLP", "ml": "ML", "ai": "AI", "mlops": "MLOps",
	"ci/cd": "CI/CD", "tdd": "TDD", "etl": "ETL", "orm": "ORM",
	"crm": "CRM", "saas": "SaaS", "ui": "UI", "ux": "UX",
	"ios": "iOS", "macos": "macOS", "postgresql": "PostgreSQL",
	"mysql": "MySQL", "mongodb": "MongoDB", "javascript": "JavaScript",
	"typescript": "TypeScript", "node.js": "Node.js", "nodejs": "Node.js",
	"c++": "C++", "c#": "C#", ".net": ".NET", "php": "PHP",
	"k8s": "Kubernetes", "github actions": "GitHub Actions",
	"gitlab ci": "GitLab CI", "dynamodb": "DynamoDB", "redis": "Redis",
	"elasticsearch": "Elasticsearch", "rabbitmq": "RabbitMQ",
	"kafka": "Kafka", "grafana": "Grafana", "prometheus": "Prometheus",
	"opentelemetry": "OpenTelemetry", "webrtc": "WebRTC",
	"websockets": "WebSockets", "wasm": "WebAssembly",
	"cdn": "CDN", "dns": "DNS", "tls": "TLS", "ssl": "SSL",
	"vpc": "VPC", "iam": "IAM", "s3": "S3", "ec2": "EC2",
	"rds": "RDS", "sre": "SRE", "qa": "QA", "abac": "ABAC", "rbac": "RBAC",
	"spa": "SPA", "ssr": "SSR", "seo": "SEO", "dsl": "DSL",
	"cqrs": "CQRS", "ddd": "DDD", "ipc": "IPC", "rpc": "RPC",
	"vue.js": "Vue.js", "next.js": "Next.js", "nuxt.js": "Nuxt.js",
}

// DisplayName is how a skill should be written on screen.
func DisplayName(canonical string) string {
	if s, ok := displayNames[canonical]; ok {
		return s
	}
	// Title-case each word, which is right for everything else in the
	// vocabulary ("distributed systems" -> "Distributed Systems").
	parts := strings.Fields(canonical)
	for i, p := range parts {
		r := []rune(p)
		parts[i] = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	return strings.Join(parts, " ")
}
