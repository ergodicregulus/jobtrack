package seed

import "github.com/jobtrack/jobtrack/internal/normalise"

// SkillCategories maps every canonical skill onto a storage category.
//
// The VOCABULARY is the source of truth for which skills exist; this function
// only says what kind each one is. Anything in the vocabulary without an
// explicit category here still gets a row, categorised "other" — because a
// missing category is a cosmetic gap, while a missing ROW silently discards
// every extraction of that skill at ingest time.
//
// That is not hypothetical. This list and the vocabulary were maintained
// separately, so expanding the vocabulary from 47 to ~120 terms changed nothing
// until they were reconciled: `posting_skills` joins the `skills` table on
// name, and an inner join drops what it cannot match without an error.
func SkillCategories() map[string]string {
	categories := map[string][]string{
		"language":  {"go", "python", "java", "javascript", "typescript", "rust", "c++", "c#", "ruby", "php", "kotlin", "swift", "scala"},
		"datastore": {"postgresql", "mysql", "mongodb", "redis", "elasticsearch", "cassandra", "dynamodb", "clickhouse"},
		"framework": {"django", "flask", "rails", "spring", "react", "vue", "svelte", "angular", "node.js"},
		"cloud":     {"kubernetes", "docker", "terraform", "aws", "gcp", "azure"},
		"messaging": {"kafka", "rabbitmq", "grpc", "graphql"},
		"practice":  {"ci/cd", "observability", "microservices", "rest", "sql", "linux", "git"},
	}
	out := make(map[string]string)
	for category, names := range categories {
		for _, n := range names {
			out[n] = category
		}
	}

	// Everything the extractor can produce must have a row, categorised or not.
	for _, canonical := range normalise.DefaultVocabulary().Canonicals() {
		if _, ok := out[canonical]; !ok {
			out[canonical] = "other"
		}
	}
	return out
}
