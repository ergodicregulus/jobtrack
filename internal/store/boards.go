package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/seed"
)

// ErrNoBoards is returned instead of pruning against an empty curated list.
var ErrNoBoards = errors.New("store: curated board list is empty; refusing to prune")

// EnsureBoards makes the sources table match the curated board list: every
// board registered, and every source no longer listed removed with its postings.
//
// It runs at ingestor startup, the same way EnsureSkills does, because the board
// list is product data that ships with the code. It used to live only in
// cmd/seed, which is in no image and refuses to run against a production
// database — so a real deployment had no way to learn which boards to poll, and
// would have come up with an empty feed and nothing to fetch.
//
// Registration is idempotent and leaves polling state alone: an existing source
// keeps its tier, validators and next poll. A new one starts at tier A so a
// fresh install fetches promptly; the retier job moves quiet boards down.
func EnsureBoards(ctx context.Context, pool *pgxpool.Pool, boards []seed.Board) (registered int, pruned int64, err error) {
	// Pruning deletes postings. Against an empty list it would delete every
	// posting there is, and this runs on every deploy — so an empty list is
	// treated as the bug it almost certainly is, not as an instruction.
	if len(boards) == 0 {
		return 0, 0, ErrNoBoards
	}

	for _, b := range boards {
		var companyID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO companies (slug, name, website, primary_domain, hq_country)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name, updated_at = now()
			RETURNING id`,
			b.Slug, b.Name, "https://"+b.Domain, b.Domain, b.Country).Scan(&companyID)
		if err != nil {
			return registered, 0, fmt.Errorf("upsert company %s: %w", b.Slug, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO sources (company_id, vendor, board_token, tier, next_poll_at)
			VALUES ($1, $2::source_vendor, $3, 'a', now())
			ON CONFLICT (vendor, board_token) DO UPDATE
			   SET company_id = EXCLUDED.company_id, updated_at = now()`,
			companyID, string(b.Vendor), b.Token); err != nil {
			return registered, 0, fmt.Errorf("upsert source %s/%s: %w", b.Vendor, b.Token, err)
		}
		registered++
	}

	pruned, err = pruneBoards(ctx, pool, boards)
	return registered, pruned, err
}

// pruneBoards removes sources that are no longer in the curated list.
//
// An earlier revision registered board tokens guessed from company names, most
// of which did not exist. Those rows stayed after the list was corrected, and a
// source that resolves to nothing is not harmless: the fetcher keeps polling it,
// the circuit breaker keeps tripping, and the error rate reads as a vendor
// outage rather than stale configuration. Its postings go too — a posting whose
// board no longer exists cannot be applied to.
func pruneBoards(ctx context.Context, pool *pgxpool.Pool, boards []seed.Board) (int64, error) {
	keep := make([]string, 0, len(boards))
	for _, b := range boards {
		keep = append(keep, string(b.Vendor)+":"+b.Token)
	}

	tag, err := pool.Exec(ctx, `
		WITH doomed AS (
			SELECT id FROM sources WHERE vendor::text || ':' || board_token <> ALL($1)
		), _p AS (
			DELETE FROM job_postings WHERE source_id IN (SELECT id FROM doomed)
		)
		DELETE FROM sources WHERE id IN (SELECT id FROM doomed)`, keep)
	if err != nil {
		return 0, fmt.Errorf("prune sources: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM companies c
		 WHERE NOT EXISTS (SELECT 1 FROM sources s WHERE s.company_id = c.id)`); err != nil {
		return 0, fmt.Errorf("prune companies: %w", err)
	}
	return tag.RowsAffected(), nil
}
