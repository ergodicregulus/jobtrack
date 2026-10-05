package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pairs are only ever drawn from DIFFERENT sources. Within one source the
// vendor's own ids are the authority: it publishes two postings because there
// are two, each with its own apply link. The version that compared postings
// inside a source hid 4,310 that were still listed on their boards, measured
// against every vendor's feed on 2026-10-05: Stripe's 718 showed as one, Brex's
// product roles in three cities as a single Vancouver posting, "Senior Software
// Engineer, iOS" as "Principal Software Engineer".
//
// The title comparison is on the raw title, lower-cased, at 0.9. The normalised
// title drops seniority, so "Senior X" and "Principal X" compared as identical;
// the same role carried by two feeds keeps its words.
const dedupeAcrossSourcesSQL = `
WITH candidates AS (
    SELECT DISTINCT ON (a.id) a.id AS loser_id, b.id AS keeper_id
      FROM job_postings a
      JOIN job_postings b
        ON b.company_id = a.company_id
       AND b.source_id <> a.source_id
       AND b.status = 'live'
     WHERE a.company_id = $1
       AND a.status = 'live'
       AND similarity(lower(a.title), lower(b.title)) >= 0.9
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
     ORDER BY a.id, similarity(lower(a.title), lower(b.title)) DESC, b.id
)
UPDATE job_postings p
   SET status = 'superseded', canonical_id = c.keeper_id, updated_at = now()
  FROM candidates c
 WHERE p.id = c.loser_id
   AND p.status = 'live'
   -- Never point at a posting that was itself just superseded, or the
   -- canonical chain becomes a linked list the feed has to walk.
   AND EXISTS (SELECT 1 FROM job_postings k WHERE k.id = c.keeper_id AND k.status = 'live')`

// DedupeAcrossSources points a company's live postings at a near-identical one
// carried by another of its sources, returning the number superseded.
//
// requisition_id is deliberately not a key. It is employer free text: Stripe
// puts "See Opening ID" on every posting, Airbnb "ONE" on 137 unrelated roles,
// and Brex reuses one id across twenty product roles.
func DedupeAcrossSources(ctx context.Context, pool *pgxpool.Pool, companyID int64) (int64, error) {
	tag, err := pool.Exec(ctx, dedupeAcrossSourcesSQL, companyID)
	if err != nil {
		return 0, fmt.Errorf("dedupe across sources: %w", err)
	}
	return tag.RowsAffected(), nil
}
