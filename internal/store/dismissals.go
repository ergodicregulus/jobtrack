package store

import (
	"context"
	"fmt"

	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DismissReasons are the reasons a person may give. Mirrors the CHECK
// constraint in migration 0022; a value not in this list is rejected here so the
// API returns a 400 rather than letting Postgres raise a constraint violation
// that would surface as a 500.
var DismissReasons = []string{
	"not_interested", "wrong_location", "wrong_level", "wrong_comp", "already_applied",
}

// Dismissal is one rejected posting, for the undo list.
type Dismissal struct {
	PostingID   int64   `db:"posting_id" json:"posting_id"`
	Title       string  `db:"title" json:"title"`
	Company     string  `db:"company" json:"company"`
	Reason      *string `db:"reason" json:"reason"`
	DismissedAt string  `db:"dismissed_at" json:"dismissed_at"`
}

// Dismiss records that a user does not want to see a posting again.
//
// Idempotent by primary key. A second dismissal updates the reason rather than
// failing: the realistic cause is someone dismissing from the feed and then
// again from the detail page with a reason attached, and an error there would
// be a bug report about a button that does nothing.
func Dismiss(ctx context.Context, pool *pgxpool.Pool, userID, postingID int64, reason string) error {
	if reason != "" && !validReason(reason) {
		return fmt.Errorf("dismiss: unknown reason %q", reason)
	}
	var r any
	if reason != "" {
		r = reason
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO dismissed_postings (user_id, posting_id, reason)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, posting_id) DO UPDATE
		   SET reason = COALESCE(EXCLUDED.reason, dismissed_postings.reason),
		       dismissed_at = now()`,
		userID, postingID, r)
	if err != nil {
		// A posting that does not exist is a 404, not a 500. The foreign key is
		// what catches it — checking first would be a second round trip and a
		// race, since a posting can be deleted between the check and the
		// insert.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return fmt.Errorf("dismiss posting %d: %w", postingID, ErrNotFound)
		}
		return fmt.Errorf("dismiss posting %d: %w", postingID, err)
	}
	return nil
}

// Undismiss reverses a dismissal.
//
// Undo is not a nicety here: dismissal is one click on a list the user is
// skimming fast, so mis-clicks are certain. A dismissal that cannot be undone
// makes people slow down and read every row before acting, which costs more
// than the feature saves.
func Undismiss(ctx context.Context, pool *pgxpool.Pool, userID, postingID int64) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM dismissed_postings WHERE user_id = $1 AND posting_id = $2`, userID, postingID)
	if err != nil {
		return fmt.Errorf("undismiss posting %d: %w", postingID, err)
	}
	return nil
}

// ListDismissals returns what a user has hidden, newest first.
//
// Bounded because this is a review list, not an archive: someone auditing what
// they hid is looking at this week, and an unbounded query here grows with a
// heavy user's entire history.
func ListDismissals(ctx context.Context, pool *pgxpool.Pool, userID int64, limit int) ([]Dismissal, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := pool.Query(ctx, `
		SELECT d.posting_id,
		       p.title,
		       c.name AS company,
		       d.reason,
		       to_char(d.dismissed_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS dismissed_at
		  FROM dismissed_postings d
		  JOIN job_postings p ON p.id = d.posting_id
		  JOIN sources s      ON s.id = p.source_id
		  JOIN companies c    ON c.id = s.company_id
		 WHERE d.user_id = $1
		 ORDER BY d.dismissed_at DESC
		 LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list dismissals: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[Dismissal])
	if err != nil {
		return nil, fmt.Errorf("list dismissals: %w", err)
	}
	return out, nil
}

func validReason(v string) bool {
	for _, r := range DismissReasons {
		if r == v {
			return true
		}
	}
	return false
}
