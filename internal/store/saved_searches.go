package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrTooManySearches is returned when a user is at the cap.
//
// Bounded because this is per-user storage: unbounded rows per account is how a
// small table becomes the largest one, which is the lesson ADR-0016 cost 1.9 GB
// to learn.
var ErrTooManySearches = errors.New("store: saved search limit reached")

// maxSavedSearches per user. Generous for a person, and a ceiling on abuse.
const maxSavedSearches = 50

// SavedSearch is a named filter, stored as the URL query string that produced it.
type SavedSearch struct {
	ID        int64     `db:"id"`
	Name      string    `db:"name"`
	Query     string    `db:"query"`
	IsDefault bool      `db:"is_default"`
	CreatedAt time.Time `db:"created_at"`

	LastRunAt *time.Time `db:"last_run_at"`

	// NewSince counts postings matching this search that were posted after the
	// last run. Computed on read rather than stored: it changes with the corpus
	// rather than with the search, so a stored value would be wrong within
	// minutes of being written.
	NewSince int `db:"new_since"`
}

// ListSavedSearches returns a user's searches, newest first, with their
// new-since-last-run counts.
//
// Ordered default-first, which is also how the caller finds the default: a
// dedicated "get my default" query existed briefly and was deleted, because the
// list already carries is_default and a second endpoint returning a subset of
// this one is a second thing to keep correct.
//
// The count is deliberately cheap and approximate in one specific way: it
// counts postings newer than the high-water mark WITHOUT re-applying the
// search's own filters, because applying them would mean parsing and executing
// fifty stored queries to render one sidebar. It answers "has anything happened
// since you last looked", which is the question the badge is asked.
func ListSavedSearches(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]SavedSearch, error) {
	rows, err := pool.Query(ctx, `
		SELECT s.id, s.name, s.query, s.is_default, s.created_at, s.last_run_at,
		       (
		         SELECT count(*) FROM job_postings p
		          WHERE p.status = 'live'
		            AND s.last_seen_max_posted_at IS NOT NULL
		            AND COALESCE(p.posted_at, p.first_seen_at) > s.last_seen_max_posted_at
		       )::int AS new_since
		  FROM saved_searches s
		 WHERE s.user_id = $1
		 ORDER BY s.is_default DESC, s.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list saved searches: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[SavedSearch])
	if err != nil {
		return nil, fmt.Errorf("list saved searches: %w", err)
	}
	return out, nil
}

// CreateSavedSearch stores a search and stamps its high-water mark.
//
// The mark is set at creation, not left null, so the first "new since" count is
// zero rather than "every posting ever". A badge that says 12,000 on the day
// you save a search is a badge nobody looks at twice.
func CreateSavedSearch(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID int64,
	name, query string,
	makeDefault bool,
) (SavedSearch, error) {
	var s SavedSearch

	err := InTx(ctx, pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM saved_searches WHERE user_id = $1`, userID).Scan(&n); err != nil {
			return fmt.Errorf("count saved searches: %w", err)
		}
		if n >= maxSavedSearches {
			return ErrTooManySearches
		}

		if makeDefault {
			// Clear the old default first: the partial unique index would
			// otherwise reject the insert, and an error is the wrong answer to
			// "make this my default".
			if _, err := tx.Exec(ctx,
				`UPDATE saved_searches SET is_default = false WHERE user_id = $1 AND is_default`,
				userID); err != nil {
				return fmt.Errorf("clear default: %w", err)
			}
		}

		return tx.QueryRow(ctx, `
			INSERT INTO saved_searches (user_id, name, query, is_default, last_run_at,
			                            last_seen_max_posted_at)
			VALUES ($1, $2, $3, $4, now(),
			        (SELECT max(COALESCE(posted_at, first_seen_at))
			           FROM job_postings WHERE status = 'live'))
			RETURNING id, name, query, is_default, created_at, last_run_at, 0`,
			userID, name, query, makeDefault).
			Scan(&s.ID, &s.Name, &s.Query, &s.IsDefault, &s.CreatedAt, &s.LastRunAt, &s.NewSince)
	})
	if err != nil {
		return SavedSearch{}, err
	}
	return s, nil
}

// DeleteSavedSearch removes one search.
func DeleteSavedSearch(ctx context.Context, pool *pgxpool.Pool, userID, id int64) error {
	tag, err := pool.Exec(ctx,
		`DELETE FROM saved_searches WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete saved search: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkSavedSearchRun advances the high-water mark after a user opens a search.
//
// Called when the search is replayed, so "new since" means "since you last
// looked at THIS search" rather than since you last visited the site. The two
// are different questions and a user with several searches notices immediately
// when they are conflated.
func MarkSavedSearchRun(ctx context.Context, pool *pgxpool.Pool, userID, id int64) error {
	_, err := pool.Exec(ctx, `
		UPDATE saved_searches
		   SET last_run_at = now(),
		       last_seen_max_posted_at = (
		         SELECT max(COALESCE(posted_at, first_seen_at))
		           FROM job_postings WHERE status = 'live'
		       )
		 WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("mark saved search run: %w", err)
	}
	return nil
}
