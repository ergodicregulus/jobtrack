package datamigrations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/normalise"
)

// ReparseLocations re-runs the location parser over stored postings.
//
// The country table knew nine countries. Sampled over 150 live postings with no
// country on 2026-09-01, 71% of them named theirs in plain English in the last
// comma-segment — "Shanghai, Shanghai, China", "Budapest, , Hungary" — and the
// lookup had no entry; a further 5% carried a state or province code that the
// parser computed and then discarded.
//
// Widening the tables fixes new postings at ingest. It does nothing for the
// thousands already stored: a posting that has dropped off its board is never
// upserted again, and until a board changes nothing revisits the rows that are
// still on it. Without this backfill the filter stays broken for most of the
// corpus on the day the fix ships.
//
// In Go, running the REAL parser, for the same reason as the field classifier:
// re-expressing a table of place names in SQL is two implementations of one
// judgement, and they drift.
type ReparseLocations struct{}

const reparseBatchSize = 2000

func (m *ReparseLocations) Version() int64 { return 104 }
func (m *ReparseLocations) Name() string   { return "reparse_locations" }

// RequiresSchemaVersion is 1: country, region and city are original columns.
func (m *ReparseLocations) RequiresSchemaVersion() int64 { return 1 }

func (m *ReparseLocations) EstimateTotal(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM job_postings
		 WHERE country IS NULL AND coalesce(location_raw, '') <> ''`).Scan(&n)
	return n, err
}

type locRow struct {
	ID  int64  `db:"id"`
	Raw string `db:"location_raw"`
}

func (m *ReparseLocations) Batch(
	ctx context.Context, pool *pgxpool.Pool, cursor []byte,
) (next []byte, rows int64, done bool, err error) {
	after := decodeCursor(cursor)

	// Only rows we failed on. A posting whose country we already hold is not
	// re-parsed: the tables only ever widen, so a successful parse cannot become
	// a better one, and re-reading every row would turn a repair into a rewrite
	// of the whole table.
	found, err := pool.Query(ctx, `
		SELECT id, coalesce(location_raw, '') AS location_raw
		  FROM job_postings
		 WHERE id > $1 AND country IS NULL AND coalesce(location_raw, '') <> ''
		 ORDER BY id
		 LIMIT $2`, after, reparseBatchSize)
	if err != nil {
		return cursor, 0, false, fmt.Errorf("read batch after id %d: %w", after, err)
	}
	batch, err := pgx.CollectRows(found, pgx.RowToStructByName[locRow])
	if err != nil {
		return cursor, 0, false, fmt.Errorf("collect batch after id %d: %w", after, err)
	}
	if len(batch) == 0 {
		return cursor, 0, true, nil
	}

	ids := make([]int64, 0, len(batch))
	countries := make([]string, 0, len(batch))
	regions := make([]string, 0, len(batch))
	cities := make([]string, 0, len(batch))
	for _, r := range batch {
		loc := normalise.ParseLocation(r.Raw)
		if loc.Country == "" {
			continue // still unparseable; leave it null rather than write a blank
		}
		ids = append(ids, r.ID)
		countries = append(countries, loc.Country)
		regions = append(regions, loc.Region)
		cities = append(cities, loc.City)
	}

	var affected int64
	if len(ids) > 0 {
		tag, err := pool.Exec(ctx, `
			UPDATE job_postings p
			   SET country = x.country,
			       region  = NULLIF(x.region, ''),
			       city    = NULLIF(x.city, ''),
			       updated_at = now()
			  FROM unnest($1::bigint[], $2::text[], $3::text[], $4::text[])
			       AS x(id, country, region, city)
			 WHERE p.id = x.id`, ids, countries, regions, cities)
		if err != nil {
			return cursor, 0, false, fmt.Errorf("write batch after id %d: %w", after, err)
		}
		affected = tag.RowsAffected()
	}

	// Advance by the window scanned, not by rows changed: most of this batch is
	// expected to stay unparseable, and a cursor tracking updates would stall on
	// the first run of them.
	return encodeCursor(batch[len(batch)-1].ID), affected, false, nil
}
