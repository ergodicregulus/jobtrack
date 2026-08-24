package normalise

import "testing"

// TestDisplayName_UsesTheConventionalWrittenForm guards names that appear on
// every card, chip and gap line in the product.
//
// These were generated with SQL initcap(), which produced "Aws", "Graphql",
// "Llm", "Node.Js" and "Postgresql". Getting a technology's name wrong on
// screen is a small thing that reads as not knowing the field, to an audience
// that works in it.
func TestDisplayName_UsesTheConventionalWrittenForm(t *testing.T) {
	cases := map[string]string{
		"aws":        "AWS",
		"graphql":    "GraphQL",
		"llm":        "LLM",
		"node.js":    "Node.js",
		"postgresql": "PostgreSQL",
		"ci/cd":      "CI/CD",
		"grpc":       "gRPC",
		"ios":        "iOS",
		"rest":       "REST",
		"typescript": "TypeScript",
		// Title-casing is correct for everything not in the override table,
		// and the fallback has to keep working.
		"docker":              "Docker",
		"kubernetes":          "Kubernetes",
		"distributed systems": "Distributed Systems",
	}
	for canonical, want := range cases {
		if got := DisplayName(canonical); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", canonical, got, want)
		}
	}
}

// Every canonical the extractor can produce must render as something, and
// never as an empty string — a blank chip is worse than an ugly one.
func TestDisplayName_CoversTheWholeVocabulary(t *testing.T) {
	for _, canonical := range DefaultVocabulary().Canonicals() {
		if got := DisplayName(canonical); got == "" {
			t.Errorf("DisplayName(%q) is empty", canonical)
		}
	}
}
