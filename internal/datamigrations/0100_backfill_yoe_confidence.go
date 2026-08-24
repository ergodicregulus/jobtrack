package datamigrations

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BackfillYoEConfidence populates job_postings.yoe_confidence for rows ingested
// before the column existed.
//
// This is the canonical shape of a data migration and exists partly as a
// worked example:
//
//   - Batched by primary key, so each batch is a bounded index range scan
//     rather than a full table scan with an OFFSET.
//   - The cursor is the last id processed, encoded as 8 bytes. Persisted after
//     every batch, so a crash resumes rather than restarting.
//   - Idempotent: the WHERE clause skips rows already populated, so re-running
//     a batch after a crash is a no-op rather than double work.
//   - Bounded: LIMIT keeps each statement short enough that it never blocks
//     autovacuum or holds a long transaction.
type BackfillYoEConfidence struct{}

const yoeBatchSize = 5000

func (m *BackfillYoEConfidence) Version() int64 { return 100 }
func (m *BackfillYoEConfidence) Name() string   { return "backfill_yoe_confidence" }

// RequiresSchemaVersion is 0003, the migration that added the column. The
// runner refuses to start before that is applied.
func (m *BackfillYoEConfidence) RequiresSchemaVersion() int64 { return 3 }

func (m *BackfillYoEConfidence) EstimateTotal(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	// An estimate from the planner rather than an exact COUNT(*): counting 200M
	// rows to render a progress bar is not a good trade.
	var estimate int64
	err := pool.QueryRow(ctx, `
		SELECT GREATEST(reltuples, 0)::bigint
		  FROM pg_class WHERE relname = 'job_postings'`).Scan(&estimate)
	return estimate, err
}

func (m *BackfillYoEConfidence) Batch(
	ctx context.Context, pool *pgxpool.Pool, cursor []byte,
) (next []byte, rows int64, done bool, err error) {
	after := decodeCursor(cursor)

	// Keyset by id, never OFFSET: OFFSET re-scans and discards everything
	// before the window, so batch N costs O(N).
	tag, err := pool.Exec(ctx, `
		WITH batch AS (
			SELECT id, yoe_min, yoe_max
			  FROM job_postings
			 WHERE id > $1
			 ORDER BY id
			 LIMIT $2
		)
		UPDATE job_postings p
		   SET yoe_confidence = CASE
		           WHEN b.yoe_min IS NULL AND b.yoe_max IS NULL THEN 0.0
		           WHEN b.yoe_min IS NOT NULL AND b.yoe_max IS NOT NULL THEN 0.9
		           ELSE 0.6
		       END
		  FROM batch b
		 WHERE p.id = b.id
		   AND p.yoe_confidence IS NULL`,
		after, yoeBatchSize)
	if err != nil {
		return cursor, 0, false, fmt.Errorf("update batch after id %d: %w", after, err)
	}

	// Advance the cursor by the window we scanned, not by the rows we changed.
	// Using rows-changed would stall forever on a run of already-populated rows.
	var maxID int64
	err = pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(id), $1) FROM (
		     SELECT id FROM job_postings WHERE id > $1 ORDER BY id LIMIT $2
		 ) w`, after, yoeBatchSize).Scan(&maxID)
	if err != nil {
		return cursor, tag.RowsAffected(), false, fmt.Errorf("advance cursor: %w", err)
	}

	// No new ids beyond the cursor means the table is exhausted.
	if maxID == after {
		return encodeCursor(maxID), tag.RowsAffected(), true, nil
	}
	return encodeCursor(maxID), tag.RowsAffected(), false, nil
}

func decodeCursor(b []byte) int64 {
	if len(b) != 8 {
		return 0
	}
	return int64(binary.BigEndian.Uint64(b))
}

func encodeCursor(id int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id))
	return b
}
