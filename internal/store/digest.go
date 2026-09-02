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
	UserID      int64     `db:"user_id"`
	Email       string    `db:"email"`
	SearchID    int64     `db:"search_id"`
	SearchName  string    `db:"search_name"`
	Query       string    `db:"query"`
	Since       time.Time `db:"since"`
	NewPostings int       `db:"new_postings"`
}

// DigestCandidates finds who has something new to hear about.
//
// THREE CONDITIONS, and each of them is a promise:
//
//   - A live, un-withdrawn `digest_email` consent. Not a flag on the user row:
//     withdrawal has to be visible as a record, which is what user_consents is
//     for and why the DPDP work put `digest_email` in its CHECK constraint
//     before there was anything to send.
//   - At least one posting newer than the mark the saved search already keeps.
//     No second definition of "new" — last_seen_max_posted_at is the same
//     boundary the badge in the sidebar uses, so the email and the interface
//     cannot disagree about what the reader has already seen.
//   - Nothing sent for that search since `notBefore`. The interval is the
//     caller's, so a weekly cadence is a scheduling decision rather than
//     something baked into SQL.
//
// An empty digest is never a candidate. "Nothing new this week" is the message
// people unsubscribe from, and on most weeks for most searches it is the honest
// content — so the query, not the template, is where that is decided.
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
		       COALESCE(s.last_seen_max_posted_at, s.created_at) AS since,
		       (SELECT count(*) FROM job_postings p
		         WHERE p.status = 'live'
		           AND p.field <> 'other'
		           AND p.posted_at > COALESCE(s.last_seen_max_posted_at, s.created_at)
		       )::int          AS new_postings
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
	all, err := pgx.CollectRows(rows, pgx.RowToStructByName[DigestCandidate])
	if err != nil {
		return nil, fmt.Errorf("digest candidates: %w", err)
	}

	// Filtered here rather than in a HAVING clause so the zero case is
	// explicit and greppable: a search with nothing new is not emailed.
	out := make([]DigestCandidate, 0, len(all))
	for _, c := range all {
		if c.NewPostings > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}

// DigestItem is one role in the email.
type DigestItem struct {
	Title    string   `db:"title"`
	Company  string   `db:"company"`
	Location string   `db:"location_raw"`
	URL      string   `db:"apply_url"`
	CompMin  *float64 `db:"comp_min"`
	Currency *string  `db:"comp_currency"`
}

// DigestItems reads the roles to name in one digest.
//
// Deliberately few. A digest is a prompt to come back, not a replacement for the
// feed, and a hundred rows in an email is a list nobody reads and a message that
// trips spam heuristics.
func DigestItems(
	ctx context.Context, pool *pgxpool.Pool, since time.Time, limit int,
) ([]DigestItem, error) {
	rows, err := pool.Query(ctx, `
		SELECT p.title, c.name AS company,
		       COALESCE(p.location_raw, '') AS location_raw,
		       p.apply_url, p.comp_min, p.comp_currency
		  FROM job_postings p
		  JOIN sources s   ON s.id = p.source_id
		  JOIN companies c ON c.id = s.company_id
		 WHERE p.status = 'live'
		   AND p.field <> 'other'
		   AND p.posted_at > $1
		 ORDER BY p.posted_at DESC
		 LIMIT $2`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("digest items: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[DigestItem])
	if err != nil {
		return nil, fmt.Errorf("digest items: %w", err)
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
