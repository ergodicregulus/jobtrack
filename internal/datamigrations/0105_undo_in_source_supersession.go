package datamigrations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UndoInSourceSupersession restores the postings dedup hid inside their own
// source.
//
// Dedup compared postings within a source, keyed on requisition_id — employer
// free text, "See Opening ID" on every one of Stripe's — and on normalised
// titles that drop seniority. Measured 2026-10-05 against every vendor's own
// feed: every superseded posting had its keeper in the same source, and 4,310
// of them were still listed on their board as a distinct job. Stripe showed
// 1 role of 718. The code no longer looks inside a source; this repairs the rows
// it already merged, which no future poll would: a board that answers 304
// re-upserts nothing.
//
// Each row goes back to what its board says. last_seen_at is stamped in the same
// transaction as the source's last_changed_at, so a row seen at or after it was
// on the board at the latest full poll and is live; a row seen before it had
// already left, and is closed now — dated to now, as 0102 does, because when it
// actually left was never recorded.
type UndoInSourceSupersession struct{}

const undoSupersessionBatchSize = 5000

func (m *UndoInSourceSupersession) Version() int64 { return 105 }
func (m *UndoInSourceSupersession) Name() string   { return "undo_in_source_supersession" }

// RequiresSchemaVersion is 1: status, canonical_id and last_seen_at are original
// columns.
func (m *UndoInSourceSupersession) RequiresSchemaVersion() int64 { return 1 }

const inSourceSupersededSQL = `
	SELECT p.id FROM job_postings p
	  JOIN job_postings k ON k.id = p.canonical_id
	 WHERE p.status = 'superseded' AND k.source_id = p.source_id`

func (m *UndoInSourceSupersession) EstimateTotal(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT count(*) FROM (`+inSourceSupersededSQL+`) x`).Scan(&n)
	return n, err
}

func (m *UndoInSourceSupersession) Batch(
	ctx context.Context, pool *pgxpool.Pool, cursor []byte,
) (next []byte, rows int64, done bool, err error) {
	after := decodeCursor(cursor)

	// The cursor advances by the rows changed: each one leaves the predicate as
	// it is repaired, so the next window cannot re-read it.
	var maxID *int64
	err = pool.QueryRow(ctx, `
		WITH batch AS (`+inSourceSupersededSQL+`
		   AND p.id > $1
		 ORDER BY p.id LIMIT $2
		), repaired AS (
		UPDATE job_postings p
		   SET status = CASE WHEN p.last_seen_at >= s.last_changed_at
		                     THEN 'live'::posting_status ELSE 'closed'::posting_status END,
		       closed_at = CASE WHEN p.last_seen_at >= s.last_changed_at
		                        THEN NULL ELSE now() END,
		       missing_count = CASE WHEN p.last_seen_at >= s.last_changed_at
		                            THEN 0 ELSE p.missing_count END,
		       canonical_id = NULL,
		       updated_at = now()
		  FROM batch b, sources s
		 WHERE p.id = b.id AND s.id = p.source_id
		RETURNING p.id)
		SELECT count(*), max(id) FROM repaired`,
		after, undoSupersessionBatchSize).Scan(&rows, &maxID)
	if err != nil {
		return cursor, 0, false, fmt.Errorf("repair batch after id %d: %w", after, err)
	}
	if maxID == nil {
		return encodeCursor(after), 0, true, nil
	}
	return encodeCursor(*maxID), rows, false, nil
}
