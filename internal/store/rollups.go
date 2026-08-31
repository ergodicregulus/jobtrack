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

// Coverage is what the corpus knows about itself, as of one moment.
//
// The homepage's signature graphic is drawn from this: every live posting is a
// mark, and a mark is hollow where we could not read the thing being asked
// about. Four dimensions rather than one because they differ by a factor of
// four — pay is known for 15% of postings and work mode for 56% — and a graphic
// that could only show the worst case would be making a point rather than
// reporting one.
type Coverage struct {
	AsOf  time.Time `json:"as_of"`
	Live  int       `json:"live"`
	Comp  int       `json:"comp"`
	YoE   int       `json:"yoe"`
	Mode  int       `json:"mode"`
	Skill int       `json:"skills"`
}

// CorpusCoverage reads the latest rolled-up day.
//
// Rows read is one per source for a single day — about 75 — and that does not
// grow with the corpus, which is the whole reason this reads source_daily
// rather than job_postings. AsOf is the rollup's own computed_at, so the page
// can say when it last looked instead of implying it is live to the second.
func CorpusCoverage(ctx context.Context, pool *pgxpool.Pool) (Coverage, error) {
	var c Coverage
	err := pool.QueryRow(ctx, `
		SELECT coalesce(max(computed_at), now()),
		       coalesce(sum(postings_live), 0)::int,
		       coalesce(sum(known_comp), 0)::int,
		       coalesce(sum(known_yoe), 0)::int,
		       coalesce(sum(known_mode), 0)::int,
		       coalesce(sum(known_skills), 0)::int
		  FROM source_daily
		 WHERE day = (SELECT max(day) FROM source_daily)`,
	).Scan(&c.AsOf, &c.Live, &c.Comp, &c.YoE, &c.Mode, &c.Skill)
	if err != nil {
		return c, fmt.Errorf("corpus coverage: %w", err)
	}
	return c, nil
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
	WITH skilled AS (
	    -- Folded once through a CTE rather than a correlated EXISTS per posting:
	    -- the rebuild runs over sources x days, so a per-row subquery would
	    -- multiply by both.
	    SELECT DISTINCT posting_id FROM posting_skills
	)
	INSERT INTO source_daily (
	    source_id, day, postings_new, postings_closed, postings_live,
	    known_comp, known_yoe, known_mode, known_skills, computed_at)
	SELECT s.id,
	       d.day::date,
	       count(*) FILTER (WHERE p.first_seen_at::date = d.day::date),
	       count(*) FILTER (WHERE p.closed_at::date = d.day::date),
	       count(*) FILTER (WHERE live.ok),
	       count(*) FILTER (WHERE live.ok AND p.comp_min IS NOT NULL),
	       count(*) FILTER (WHERE live.ok AND (p.yoe_min IS NOT NULL OR p.yoe_max IS NOT NULL)),
	       count(*) FILTER (WHERE live.ok AND p.mode IS NOT NULL AND p.mode <> 'unknown'),
	       count(*) FILTER (WHERE live.ok AND sk.posting_id IS NOT NULL),
	       now()
	  FROM sources s
	  CROSS JOIN generate_series($1::date, current_date, interval '1 day') AS d(day)
	  LEFT JOIN job_postings p ON p.source_id = s.id
	  LEFT JOIN skilled sk ON sk.posting_id = p.id
	  -- "Live at the end of this day" is needed by five of the counts, so it is
	  -- computed once rather than repeated in each FILTER.
	  --
	  -- SUPERSEDED POSTINGS ARE EXCLUDED, and leaving them in was a shipped bug.
	  -- A superseded posting is one dedup found to be a duplicate of another; it
	  -- never gets a closed_at, because it was not closed — it was merged. The
	  -- test used to be closed_at IS NULL alone, which counted all 5,371 of
	  -- them, so the homepage chart read "18,102 live now" directly beneath a
	  -- hero that read "13k live postings" from /v1/market. Same page, same word,
	  -- 38% apart.
	  --
	  -- The feed has always filtered status = 'live', so the corpus a reader
	  -- can actually browse never included these. The rollup is what disagreed.
	  LEFT JOIN LATERAL (
	      SELECT p.status <> 'superseded'
	         AND p.first_seen_at::date <= d.day::date
	         AND (p.closed_at IS NULL OR p.closed_at::date > d.day::date) AS ok
	  ) live ON true
	 GROUP BY s.id, d.day
	ON CONFLICT (source_id, day) DO UPDATE
	   SET postings_new    = EXCLUDED.postings_new,
	       postings_closed = EXCLUDED.postings_closed,
	       postings_live   = EXCLUDED.postings_live,
	       known_comp      = EXCLUDED.known_comp,
	       known_yoe       = EXCLUDED.known_yoe,
	       known_mode      = EXCLUDED.known_mode,
	       known_skills    = EXCLUDED.known_skills,
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
