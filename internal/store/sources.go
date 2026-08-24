package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/source"
)

const (
	claimDueSourcesSQL = `
		WITH due AS (
			SELECT id FROM sources
			 WHERE next_poll_at <= now()
			   AND tier <> 'paused'
			   AND (disabled_until IS NULL OR disabled_until < now())
			 ORDER BY next_poll_at
			 LIMIT $1
			 FOR UPDATE SKIP LOCKED
		)
		UPDATE sources s
		   SET next_poll_at = now() + CASE s.tier
		         WHEN 'a' THEN $2::interval
		         WHEN 'b' THEN $3::interval
		         ELSE $4::interval
		       END
		         -- Jitter so 500 tier-A sources do not all fire on the hour.
		         + (random() * interval '5 minutes'),
		       updated_at = now()
		  FROM due
		 WHERE s.id = due.id
		RETURNING s.id`

	loadSourceSQL = `
		SELECT id, company_id, vendor::text, board_token, etag, last_modified,
		       content_hash, detail_cursor
		  FROM sources WHERE id = $1`

	markPollSucceededSQL = `
			UPDATE sources
			   SET last_polled_at = now(), last_changed_at = now(),
			       consecutive_errors = 0, disabled_until = NULL,
			       etag = NULLIF($2,''), last_modified = NULLIF($3,''),
			       content_hash = $4, detail_cursor = $5, updated_at = now()
			 WHERE id = $1`

	markPollFailedSQL = `
		UPDATE sources
		   SET consecutive_errors = consecutive_errors + 1,
		       last_polled_at = now(),
		       disabled_until = CASE
		           WHEN $2::interval > interval '0' THEN now() + $2::interval
		           WHEN consecutive_errors + 1 >= 5 THEN now() + interval '6 hours'
		           ELSE disabled_until
		       END,
		       updated_at = now()
		 WHERE id = $1`
)

// ClaimDueSources claims sources ready to poll and pushes next_poll_at forward
// in one statement, returning the claimed ids.
//
// One statement, not a SELECT then an UPDATE: between the two, a second
// scheduler tick would pick up the same rows before the first had marked them,
// producing duplicate fetches — the politeness violation this design exists to
// prevent. FOR UPDATE SKIP LOCKED is what makes concurrent schedulers safe.
func ClaimDueSources(
	ctx context.Context,
	pool *pgxpool.Pool,
	limit int,
	tierA, tierB, tierC string,
) ([]int64, error) {
	rows, err := pool.Query(ctx, claimDueSourcesSQL, limit, tierA, tierB, tierC)
	if err != nil {
		return nil, fmt.Errorf("claim due sources: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("claim due sources: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LoadSource reads a source and its conditional-request state.
//
// A source deleted between scheduling and running returns ErrNotFound, which
// the caller treats as a no-op rather than a failure.
func LoadSource(ctx context.Context, pool *pgxpool.Pool, id int64) (source.Source, error) {
	var (
		src     source.Source
		vendor  string
		etag    *string
		lastMod *string
		hash    []byte
		cursor  int
	)
	err := pool.QueryRow(ctx, loadSourceSQL, id).
		Scan(&src.ID, &src.CompanyID, &vendor, &src.BoardToken, &etag, &lastMod, &hash, &cursor)
	if errors.Is(err, pgx.ErrNoRows) {
		return src, ErrNotFound
	}
	if err != nil {
		return src, fmt.Errorf("load source %d: %w", id, err)
	}

	src.Vendor = source.Vendor(vendor)
	src.ETag = derefString(etag)
	src.LastModified = derefString(lastMod)
	src.ContentHash = hash
	src.DetailCursor = cursor
	return src, nil
}

// MarkPollSucceeded records a successful poll and its new validators.
func MarkPollSucceeded(
	ctx context.Context,
	tx pgx.Tx,
	sourceID int64,
	etag, lastModified string,
	contentHash []byte,
	detailCursor int,
) error {
	_, err := tx.Exec(ctx, markPollSucceededSQL,
		sourceID, etag, lastModified, contentHash, detailCursor)
	if err != nil {
		return fmt.Errorf("mark poll succeeded for source %d: %w", sourceID, err)
	}
	return nil
}

// MarkPollFailed advances the circuit breaker.
//
// Five consecutive failures pause the source for six hours, so one broken board
// cannot consume a worker slot forever.
func MarkPollFailed(ctx context.Context, pool *pgxpool.Pool, sourceID int64, disableFor string) error {
	if _, err := pool.Exec(ctx, markPollFailedSQL, sourceID, disableFor); err != nil {
		return fmt.Errorf("mark poll failed for source %d: %w", sourceID, err)
	}
	return nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

const retierSourcesSQL = `
		UPDATE sources s
		   SET tier = CASE
		       -- Someone is watching this company: poll it every 2 hours.
		       WHEN EXISTS (SELECT 1 FROM watched_companies wc WHERE wc.company_id = s.company_id)
		            THEN 'a'::source_tier
		       WHEN s.last_changed_at > now() - interval '7 days'  THEN 'a'::source_tier
		       WHEN s.last_changed_at > now() - interval '30 days' THEN 'b'::source_tier
		       ELSE 'c'::source_tier
		   END,
		   updated_at = now()
		 WHERE s.tier <> 'paused'`

// RetierSources moves sources between polling tiers.
func RetierSources(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx, retierSourcesSQL)
	if err != nil {
		return 0, fmt.Errorf("retier sources: %w", err)
	}
	return tag.RowsAffected(), nil
}

const markPollUnchangedSQL = `
			UPDATE sources
			   SET last_polled_at = now(), consecutive_errors = 0,
			       etag = COALESCE(NULLIF($2,''), etag),
			       last_modified = COALESCE(NULLIF($3,''), last_modified),
			       updated_at = now()
			 WHERE id = $1`

// MarkPollUnchanged records a poll that returned no new content.
//
// The common path — roughly 90% of polls at steady state. Validators are
// refreshed if the server sent new ones, and nothing else is touched:
// last_changed_at must not move, or every freshness figure derived from it
// would report a corpus that is constantly changing when it is not.
func MarkPollUnchanged(ctx context.Context, pool *pgxpool.Pool, sourceID int64, etag, lastModified string) error {
	_, err := pool.Exec(ctx, markPollUnchangedSQL, sourceID, etag, lastModified)
	if err != nil {
		return fmt.Errorf("mark poll unchanged for source %d: %w", sourceID, err)
	}
	return nil
}
