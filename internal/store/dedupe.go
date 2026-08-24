package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const dedupeBySimilaritySQL = `
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

// DedupeBySimilarity points near-identical live postings at a canonical one,
// returning the number superseded.
func DedupeBySimilarity(ctx context.Context, pool *pgxpool.Pool, companyID int64) (int64, error) {
	tag, err := pool.Exec(ctx, dedupeBySimilaritySQL, companyID)
	if err != nil {
		return 0, fmt.Errorf("dedupe by similarity: %w", err)
	}
	return tag.RowsAffected(), nil
}

const dedupeByRequisitionSQL = `
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

// DedupeByRequisition collapses postings sharing a requisition id.
//
// Free dedup where the vendor exposes one: two postings from a company with the
// same requisition are the same role, with no similarity computation needed.
func DedupeByRequisition(ctx context.Context, pool *pgxpool.Pool, companyID int64) (int64, error) {
	tag, err := pool.Exec(ctx, dedupeByRequisitionSQL, companyID)
	if err != nil {
		return 0, fmt.Errorf("dedupe by requisition: %w", err)
	}
	return tag.RowsAffected(), nil
}
