package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SourceDay is one source's ingest activity on one day.
type SourceDay struct {
	SourceID       int64     `db:"source_id"`
	Vendor         string    `db:"vendor"`
	CompanyName    string    `db:"company_name"`
	Day            time.Time `db:"day"`
	PostingsNew    int       `db:"postings_new"`
	PostingsClosed int       `db:"postings_closed"`
	PostingsLive   int       `db:"postings_live"`
}

// rebuildSourceDailySQL recomputes the rollup from job_postings alone.
//
// One statement, and that is ADR-0017's rule rather than a flourish: a rollup
// that can be rebuilt is a cache, and a bug in it is fixed by running this
// again. A rollup that can only be accumulated is a system of record, and a bug
// in it needs a migration and an apology.
//
// The day axis comes from generate_series, not from the postings, so a day on
// which a source saw nothing produces a row of zeroes rather than no row. The
// distinction matters on a chart: a gap and a zero look different and mean
// different things, and the widget can only tell them apart if the zero is
// present.
const rebuildSourceDailySQL = `
	INSERT INTO source_daily (source_id, day, postings_new, postings_closed, postings_live, computed_at)
	SELECT s.id,
	       d.day::date,
	       count(*) FILTER (WHERE p.first_seen_at::date = d.day::date),
	       count(*) FILTER (WHERE p.closed_at::date = d.day::date),
	       count(*) FILTER (
	           WHERE p.first_seen_at::date <= d.day::date
	             AND (p.closed_at IS NULL OR p.closed_at::date > d.day::date)
	       ),
	       now()
	  FROM sources s
	  CROSS JOIN generate_series($1::date, current_date, interval '1 day') AS d(day)
	  LEFT JOIN job_postings p ON p.source_id = s.id
	 GROUP BY s.id, d.day
	ON CONFLICT (source_id, day) DO UPDATE
	   SET postings_new    = EXCLUDED.postings_new,
	       postings_closed = EXCLUDED.postings_closed,
	       postings_live   = EXCLUDED.postings_live,
	       computed_at     = now()`

// RebuildSourceDaily recomputes the rollup for the last `days` days.
//
// Idempotent, so the maintenance job can re-run a window rather than tracking
// which days it has done. A short window is the daily case; a long one is how a
// bug gets fixed.
func RebuildSourceDaily(ctx context.Context, pool *pgxpool.Pool, days int) (int64, error) {
	from := time.Now().AddDate(0, 0, -days)
	tag, err := pool.Exec(ctx, rebuildSourceDailySQL, from)
	if err != nil {
		return 0, fmt.Errorf("rebuild source_daily: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SourceDailySeries reads the rollup for a date range, newest last.
//
// Joined to sources and companies here rather than in the caller: the chart
// needs a label per line, and a widget that has to make a second query per
// series is a widget that gets slower as coverage grows.
func SourceDailySeries(ctx context.Context, pool *pgxpool.Pool, days int) ([]SourceDay, error) {
	rows, err := pool.Query(ctx, `
		SELECT sd.source_id,
		       s.vendor::text AS vendor,
		       c.name         AS company_name,
		       sd.day,
		       sd.postings_new,
		       sd.postings_closed,
		       sd.postings_live
		  FROM source_daily sd
		  JOIN sources   s ON s.id = sd.source_id
		  JOIN companies c ON c.id = s.company_id
		 WHERE sd.day >= current_date - $1::int
		 ORDER BY sd.day, sd.source_id`, days)
	if err != nil {
		return nil, fmt.Errorf("source daily series: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[SourceDay])
	if err != nil {
		return nil, fmt.Errorf("source daily series: %w", err)
	}
	return out, nil
}
