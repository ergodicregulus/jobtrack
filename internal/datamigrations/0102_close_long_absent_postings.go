package datamigrations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CloseLongAbsentPostings closes the postings ReconcileAbsent should have closed
// and never did.
//
// ReconcileAbsent bumped missing_count in a data-modifying CTE and then updated
// the same rows again in the outer statement to set status='closed'. PostgreSQL
// applies only one modification per row per command, so the close never took
// effect while the counter kept rising. The function returned a closure count
// taken from the command tag — which matched — so it reported work it had not
// done, for its entire life.
//
// The result: NOT ONE POSTING HAD EVER BEEN CLOSED. Measured 2026-09-01, 2,020
// live postings were absent from their boards, one of them for 29 consecutive
// polls, and every one was still being served, scored and shown to users as a
// role they could apply for. It is also most of why 42% of the corpus read as
// over 60 days old.
//
// The fix makes future polls correct. It does nothing for rows already past the
// threshold: they are absent, so no poll will ever upsert them again, and
// nothing else in the system revisits a live posting. Without this backfill they
// stay live forever.
//
// A data migration rather than a schema one, and not because it is slow — it
// would finish in a second today. It is here because the population grows with
// the corpus and a statement that is instant at 14,000 rows is not necessarily
// instant at ten million, and because the resume cursor makes it safe to
// interrupt. The rule in this package's doc comment is about the largest table
// it touches, and job_postings is that table.
type CloseLongAbsentPostings struct{}

const closeAbsentBatchSize = 5000

// absenceLimit is duplicated from internal/store deliberately.
//
// Importing store here would make a backfill depend on the live read/write path,
// so that a change to a query could quietly alter what a historical repair
// means. This backfill repairs rows against the threshold as it stood when they
// were orphaned; if the store's limit later changes, this one must not follow.
const absenceLimit = 2

func (m *CloseLongAbsentPostings) Version() int64 { return 102 }
func (m *CloseLongAbsentPostings) Name() string   { return "close_long_absent_postings" }

// RequiresSchemaVersion is 1: status, closed_at and missing_count are original
// columns. This repairs data, not a newly-added column.
func (m *CloseLongAbsentPostings) RequiresSchemaVersion() int64 { return 1 }

func (m *CloseLongAbsentPostings) EstimateTotal(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM job_postings
		 WHERE status = 'live' AND missing_count >= $1`, absenceLimit).Scan(&n)
	return n, err
}

func (m *CloseLongAbsentPostings) Batch(
	ctx context.Context, pool *pgxpool.Pool, cursor []byte,
) (next []byte, rows int64, done bool, err error) {
	after := decodeCursor(cursor)

	// closed_at is set to now() rather than to when the posting actually went
	// missing, because we do not know when that was — missing_count counts polls,
	// not time, and the poll timestamps were never recorded per posting. Dating
	// these closures to the moment we noticed is the honest option; inventing a
	// closure date from missing_count x the tier interval would be a fabricated
	// timestamp on 2,020 rows, and the age of a posting is the signal this
	// product is built on.
	tag, err := pool.Exec(ctx, `
		WITH batch AS (
			SELECT id FROM job_postings
			 WHERE id > $1 AND status = 'live' AND missing_count >= $3
			 ORDER BY id LIMIT $2
		)
		UPDATE job_postings p
		   SET status = 'closed', closed_at = now(), updated_at = now()
		  FROM batch b
		 WHERE p.id = b.id`,
		after, closeAbsentBatchSize, absenceLimit)
	if err != nil {
		return cursor, 0, false, fmt.Errorf("close batch after id %d: %w", after, err)
	}

	// Advance by the window scanned, not by rows changed. Here they happen to be
	// equal because the predicate and the update agree, but a cursor that
	// depends on that stalls the moment they stop agreeing.
	var maxID int64
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(id), $1) FROM (
		    SELECT id FROM job_postings
		     WHERE id > $1 AND missing_count >= $3
		     ORDER BY id LIMIT $2
		) w`, after, closeAbsentBatchSize, absenceLimit).Scan(&maxID)
	if err != nil {
		return cursor, tag.RowsAffected(), false, fmt.Errorf("advance cursor: %w", err)
	}

	if maxID == after {
		return encodeCursor(maxID), tag.RowsAffected(), true, nil
	}
	return encodeCursor(maxID), tag.RowsAffected(), false, nil
}
