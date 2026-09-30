package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DigestCandidate is one saved search worth emailing about.
type DigestCandidate struct {
	UserID     int64     `db:"user_id"`
	Email      string    `db:"email"`
	SearchID   int64     `db:"search_id"`
	SearchName string    `db:"search_name"`
	Query      string    `db:"query"`
	Since      time.Time `db:"since"`
}

// DigestCandidates finds the saved searches due a digest.
//
// Two conditions, and each of them is a promise:
//
//   - A live, un-withdrawn `digest_email` consent. Not a flag on the user row:
//     withdrawal has to be visible as a record, which is what user_consents is
//     for and why the DPDP work put `digest_email` in its CHECK constraint
//     before there was anything to send.
//   - Nothing sent for that search since `notBefore`. The interval is the
//     caller's, so a weekly cadence is a scheduling decision rather than
//     something baked into SQL.
//
// Whether anything is new is NOT decided here. It used to be, by counting every
// live posting in the corpus — which ignored the search, so a search matching
// nothing new was still emailed. The worker replays each search through Feed
// and skips the empty ones.
//
// Since is the boundary it replays from: the last_seen_max_posted_at mark the
// sidebar badge uses, so the email and the interface agree about what the reader
// has already seen — or the last digest, if later. Without that, a reader who
// did not open the site between two digests was sent the same roles twice.
func DigestCandidates(
	ctx context.Context, pool *pgxpool.Pool, notBefore time.Time, limit int,
) ([]DigestCandidate, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	rows, err := pool.Query(ctx, `
		SELECT u.id            AS user_id,
		       u.email         AS email,
		       s.id            AS search_id,
		       s.name          AS search_name,
		       s.query         AS query,
		       GREATEST(COALESCE(s.last_seen_max_posted_at, s.created_at),
		                s.last_digest_at) AS since
		  FROM saved_searches s
		  JOIN users u ON u.id = s.user_id
		 WHERE u.deleted_at IS NULL
		   AND EXISTS (
		         SELECT 1 FROM user_consents c
		          WHERE c.user_id = u.id
		            AND c.purpose = 'digest_email'
		            AND c.withdrawn_at IS NULL)
		   AND (s.last_digest_at IS NULL OR s.last_digest_at < $1)
		 ORDER BY u.id, s.id
		 LIMIT $2`, notBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("digest candidates: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[DigestCandidate])
	if err != nil {
		return nil, fmt.Errorf("digest candidates: %w", err)
	}
	return out, nil
}

// MarkDigestSent records that a search's digest went out.
//
// Written in the same statement that advances nothing else, and AFTER the send
// rather than before: a crash between the two resends a digest, which is a
// nuisance, where the other order silently drops one, which is a promise broken
// with no trace.
func MarkDigestSent(ctx context.Context, pool *pgxpool.Pool, searchID int64) error {
	_, err := pool.Exec(ctx,
		`UPDATE saved_searches SET last_digest_at = now() WHERE id = $1`, searchID)
	if err != nil {
		return fmt.Errorf("mark digest sent %d: %w", searchID, err)
	}
	return nil
}
