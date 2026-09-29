package datamigrations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/source/smartrecruiters"
)

// BackfillSmartRecruitersURLs gives stored SmartRecruiters postings the public
// URL they were ingested without.
//
// SmartRecruiters serves the board list and the posting bodies separately, and
// only the body carries applyUrl and postingUrl. The adapter now derives the
// public address from the board token and the posting id, so newly-ingested
// postings always have a link — but 5,050 rows were already stored without one,
// which is a third of the live corpus sitting in the feed with nothing to
// click. The apply link is the product's entire output.
//
// A re-poll fixes any posting still on the board. It does NOT fix a posting
// that has dropped off it and is waiting out its missing_count before closing:
// those are still served as live, still shown to users, and will never be
// upserted again. This backfill is for them.
//
// The derivation is the vendor's own canonical address —
// jobs.smartrecruiters.com/{board}/{id} — verified 200 against a live posting
// on 2026-08-24. The title slug the detail document appends is decorative.
type BackfillSmartRecruitersURLs struct{}

const srURLBatchSize = 5000

func (m *BackfillSmartRecruitersURLs) Version() int64 { return 101 }
func (m *BackfillSmartRecruitersURLs) Name() string   { return "backfill_smartrecruiters_urls" }

// RequiresSchemaVersion is 1: apply_url and posting_url are original columns.
// This backfill repairs data, not a newly-added column.
func (m *BackfillSmartRecruitersURLs) RequiresSchemaVersion() int64 { return 1 }

func (m *BackfillSmartRecruitersURLs) EstimateTotal(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	// An exact count is affordable here, unlike the yoe backfill: the predicate
	// is selective and indexed by source, and the population is thousands
	// rather than millions.
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT count(*)
		  FROM job_postings p
		  JOIN sources s ON s.id = p.source_id
		 WHERE s.vendor = 'smartrecruiters'
		   AND COALESCE(p.apply_url, '') = ''`).Scan(&n)
	return n, err
}

func (m *BackfillSmartRecruitersURLs) Batch(
	ctx context.Context, pool *pgxpool.Pool, cursor []byte,
) (next []byte, rows int64, done bool, err error) {
	after := decodeCursor(cursor)

	// Keyset by id, never OFFSET. The URL is built in SQL so the batch stays one
	// statement, but the host comes from the adapter's own constant rather than
	// being spelled a second time here.
	tag, err := pool.Exec(ctx, `
		WITH batch AS (
			SELECT p.id, s.board_token, p.external_id
			  FROM job_postings p
			  JOIN sources s ON s.id = p.source_id
			 WHERE p.id > $1
			   AND s.vendor = 'smartrecruiters'
			 ORDER BY p.id
			 LIMIT $2
		)
		UPDATE job_postings p
		   SET apply_url   = COALESCE(NULLIF(p.apply_url, ''),
		                              $3 || '/' || b.board_token || '/' || b.external_id),
		       posting_url = COALESCE(NULLIF(p.posting_url, ''),
		                              $3 || '/' || b.board_token || '/' || b.external_id),
		       updated_at  = now()
		  FROM batch b
		 WHERE p.id = b.id
		   AND (COALESCE(p.apply_url, '') = '' OR COALESCE(p.posting_url, '') = '')
		   AND b.board_token <> ''
		   AND b.external_id <> ''`,
		after, srURLBatchSize, smartrecruiters.PublicBase)
	if err != nil {
		return cursor, 0, false, fmt.Errorf("update batch after id %d: %w", after, err)
	}

	// Advance by the window scanned, not by rows changed: a run of rows that
	// already have a URL would otherwise stall the cursor forever.
	var maxID int64
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(id), $1) FROM (
		    SELECT p.id
		      FROM job_postings p
		      JOIN sources s ON s.id = p.source_id
		     WHERE p.id > $1 AND s.vendor = 'smartrecruiters'
		     ORDER BY p.id LIMIT $2
		) w`, after, srURLBatchSize).Scan(&maxID)
	if err != nil {
		return cursor, tag.RowsAffected(), false, fmt.Errorf("advance cursor: %w", err)
	}

	if maxID == after {
		return encodeCursor(maxID), tag.RowsAffected(), true, nil
	}
	return encodeCursor(maxID), tag.RowsAffected(), false, nil
}
