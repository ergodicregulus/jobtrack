package jobs

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"
)

// DedupeCompanyWorker collapses duplicate postings within one company.
//
// One requisition legitimately appears more than once: a company's Greenhouse
// board and its JSON-LD careers page describe the same role, and a migration
// between ATS vendors briefly doubles everything. Showing it twice degrades the
// feed; showing it three times destroys trust in it.
//
// Two stages, cheapest first — the shape the Fraunhofer duplicate-detection work
// validated, whose central finding is that no single technique performs
// acceptably alone:
//
//  1. Exact:  same company + same requisition_id. Free, and certain.
//  2. Fuzzy:  trigram blocking on the normalised title, then a conservative
//     similarity floor plus location and work-mode agreement.
//
// The embedding stage described in the design is deliberately NOT here yet. A
// stage that does not exist must not silently pass everything through, and
// stages 1-2 resolve the large majority.
type DedupeCompanyWorker struct {
	river.WorkerDefaults[DedupeCompanyArgs]
	Deps *Deps
}

func (w *DedupeCompanyWorker) Work(ctx context.Context, job *river.Job[DedupeCompanyArgs]) error {
	exact, err := w.mergeByRequisition(ctx, job.Args.CompanyID)
	if err != nil {
		return err
	}
	fuzzy, err := w.mergeBySimilarity(ctx, job.Args.CompanyID)
	if err != nil {
		return err
	}

	// Only log when something happened. "deduped 0" every fifteen minutes for
	// every company is exactly the clutter that hides real signals.
	if exact+fuzzy > 0 {
		w.Deps.Log.InfoContext(ctx, "postings deduplicated",
			"company_id", job.Args.CompanyID,
			"by_requisition", exact, "by_similarity", fuzzy)
	}
	return nil
}

// mergeByRequisition is the free case.
//
// Greenhouse exposes requisition_id. Two live postings from one company sharing
// it ARE the same role — no similarity computation, no threshold, no judgement
// call that could be wrong.
func (w *DedupeCompanyWorker) mergeByRequisition(ctx context.Context, companyID int64) (int64, error) {
	const q = `
WITH ranked AS (
    SELECT p.id,
           first_value(p.id) OVER (
               PARTITION BY p.requisition_id
               ORDER BY
                   -- A direct ATS record beats a scraped careers page: its
                   -- apply URL is more certainly the live requisition.
                   (CASE WHEN s.vendor = 'jsonld' THEN 1 ELSE 0 END),
                   -- Structured compensation beats parsed beats absent.
                   (CASE WHEN p.comp_src = 'structured' THEN 0
                         WHEN p.comp_min IS NOT NULL THEN 1 ELSE 2 END),
                   p.parse_confidence DESC,
                   length(coalesce(p.description_text, '')) DESC,
                   p.first_seen_at ASC,
                   p.id ASC
           ) AS keeper
      FROM job_postings p
      JOIN sources s ON s.id = p.source_id
     WHERE p.company_id = $1
       AND p.status = 'live'
       AND p.requisition_id IS NOT NULL
       AND p.requisition_id <> ''
)
UPDATE job_postings p
   SET status = 'superseded', canonical_id = r.keeper, updated_at = now()
  FROM ranked r
 WHERE p.id = r.id AND r.keeper <> r.id`

	tag, err := w.Deps.Pool.Exec(ctx, q, companyID)
	if err != nil {
		return 0, fmt.Errorf("dedupe by requisition: %w", err)
	}
	return tag.RowsAffected(), nil
}

// mergeBySimilarity handles postings with no shared requisition id.
//
// Blocking comes first: only pairs with high title-trigram similarity are ever
// compared, which is what keeps this from being O(n²) over a 500-role board.
//
// The 0.75 floor is deliberately conservative. A wrong merge HIDES a real job
// from a user, which is far worse than showing one near-duplicate — so the
// threshold is set to under-merge rather than over-merge.
func (w *DedupeCompanyWorker) mergeBySimilarity(ctx context.Context, companyID int64) (int64, error) {
	const q = `
WITH candidates AS (
    SELECT DISTINCT ON (a.id) a.id AS loser_id, b.id AS keeper_id
      FROM job_postings a
      JOIN job_postings b
        ON b.company_id = a.company_id
       AND b.id <> a.id
       AND b.status = 'live'
     WHERE a.company_id = $1
       AND a.status = 'live'
       -- Blocking. The trigram GIN index makes this cheap; without it the
       -- join is a cross product over the whole company.
       AND similarity(a.title_normalised, b.title_normalised) > 0.75
       -- Same place, or at least one side did not say. "Bengaluru" and
       -- "London" with the same title are two real jobs, not a duplicate.
       AND (a.city IS NOT DISTINCT FROM b.city OR a.city IS NULL OR b.city IS NULL)
       AND (a.mode = b.mode OR a.mode = 'unknown' OR b.mode = 'unknown')
       -- b must be strictly the better record, which also guarantees the
       -- relation is antisymmetric: two postings can never supersede each other.
       AND (b.parse_confidence,
            length(coalesce(b.description_text, '')),
            -extract(epoch FROM b.first_seen_at),
            -b.id)
         > (a.parse_confidence,
            length(coalesce(a.description_text, '')),
            -extract(epoch FROM a.first_seen_at),
            -a.id)
     ORDER BY a.id, similarity(a.title_normalised, b.title_normalised) DESC, b.id
)
UPDATE job_postings p
   SET status = 'superseded', canonical_id = c.keeper_id, updated_at = now()
  FROM candidates c
 WHERE p.id = c.loser_id
   AND p.status = 'live'
   -- Never point at a posting that was itself just superseded, or the
   -- canonical chain becomes a linked list the feed has to walk.
   AND EXISTS (SELECT 1 FROM job_postings k WHERE k.id = c.keeper_id AND k.status = 'live')`

	tag, err := w.Deps.Pool.Exec(ctx, q, companyID)
	if err != nil {
		return 0, fmt.Errorf("dedupe by similarity: %w", err)
	}
	return tag.RowsAffected(), nil
}
