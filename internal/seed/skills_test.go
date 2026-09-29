package seed

import (
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/normalise"
)

// TestSkillCategories_CoversTheWholeVocabulary is a regression test for a
// silent data-loss bug.
//
// `posting_skills` is written with an INNER JOIN onto the `skills` table:
//
//	JOIN skills s ON s.canonical = x.canonical
//
// An inner join drops what it cannot match, without an error and without a log
// line. So any skill the extractor successfully found, but which had no row in
// `skills`, was discarded on the hot ingestion path.
//
// The vocabulary and this category map were maintained as two separate lists.
// Expanding the vocabulary from 47 to ~120 terms therefore changed nothing
// measurable — extraction improved and the improvement was thrown away at the
// join. The vocabulary is now the single source of truth and this test is what
// keeps it that way.
func TestSkillCategories_CoversTheWholeVocabulary(t *testing.T) {
	categories := SkillCategories()

	var missing []string
	for _, canonical := range normalise.DefaultVocabulary().Canonicals() {
		if _, ok := categories[canonical]; !ok {
			missing = append(missing, canonical)
		}
	}

	if len(missing) > 0 {
		t.Errorf("%d vocabulary skills have no row in the skills table and would be "+
			"silently dropped at ingest: %v", len(missing), missing)
	}
}

// The reverse direction is a smaller problem — a category for a skill the
// extractor can never produce is dead weight rather than data loss — but it is
// still a list drifting out of step with the thing it describes.
func TestSkillCategories_HasNoEntriesTheExtractorCannotProduce(t *testing.T) {
	known := make(map[string]struct{})
	for _, c := range normalise.DefaultVocabulary().Canonicals() {
		known[c] = struct{}{}
	}

	var orphans []string
	for name := range SkillCategories() {
		if _, ok := known[name]; !ok {
			orphans = append(orphans, name)
		}
	}

	if len(orphans) > 0 {
		t.Errorf("categorised but not in the vocabulary, so never extractable: %v", orphans)
	}
}
