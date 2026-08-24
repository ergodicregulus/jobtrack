package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Dashboard is every figure the logged-in home page needs, read in one round trip.
type Dashboard struct {
	Matches    MatchCounts
	Pipeline   map[string]int
	NeedsReply []ActionItem
	TopMatches []TopMatch
	Market     MarketSummary
}

// MatchCounts summarises a user's scored corpus.
type MatchCounts struct {
	Strong    int
	Plausible int
	NewToday  int
	Scored    int
}

// ActionItem is an application that has gone quiet or has a date attached.
type ActionItem struct {
	ID          int64      `db:"id"`
	CompanyName string     `db:"company_name"`
	RoleTitle   string     `db:"role_title"`
	Status      string     `db:"status"`
	DaysSince   int        `db:"days_since"`
	NextAction  string     `db:"next_action"`
	NextAt      *time.Time `db:"next_action_at"`
}

// TopMatch is one of the highest-scoring live postings for a user.
type TopMatch struct {
	ID          int64     `db:"id"`
	Title       string    `db:"title"`
	CompanyName string    `db:"company_name"`
	Location    string    `db:"location"`
	Mode        string    `db:"mode"`
	Score       float64   `db:"score"`
	Band        string    `db:"band"`
	Missing     []string  `db:"missing_skills"`
	PostedAt    time.Time `db:"posted_at"`
	ApplyURL    string    `db:"apply_url"`
	Saved       bool      `db:"saved"`
}

// MarketSummary describes the whole live corpus, independent of any user.
type MarketSummary struct {
	LivePostings  int
	Companies     int
	AddedThisWeek int
	RemoteShare   float64
}

// marketSummarySQL is the one definition of the corpus figures, shared by the
// dashboard and the public landing page so the two can never quote different
// numbers for the same thing.
//
// posted_at, not first_seen_at. first_seen_at records when WE fetched a
// posting, so after a backfill every row in the database looks like it arrived
// this week — which turned "added this week" into a restatement of the corpus
// size and the dashboard into a liar.
const marketSummarySQL = `
	SELECT count(*),
	       count(DISTINCT company_id),
	       count(*) FILTER (
	           WHERE COALESCE(posted_at, first_seen_at) > now() - interval '7 days'
	       ),
	       COALESCE(avg((mode = 'remote')::int), 0)
	  FROM job_postings WHERE status = 'live'`

// The band, new-today and total counts are three statements rather than one,
// and the reason is the shape of the table.
//
// They used to be four `count(*) FILTER (...)` aggregates over a single join,
// which meant reading every one of a user's ~15,000 score rows to evaluate a
// filter matching a few hundred of them. On a table rewritten as often as this
// one — every rescore replaces every row a user has — the heap is never clean
// enough for an index-only scan to hold, so that read degraded to 12,149 heap
// fetches and 13.7 seconds, and the dashboard returned 500 at its ten-second
// deadline.
//
// Split, each part reads only what it needs: 21ms + 79ms + 285ms measured
// 2026-08-24, against 3,565ms fused on the same data. They stay in one batch,
// so this is still a single network round trip.
const (
	// Found by range scan rather than by filtering the whole set. No join: a
	// score row for a non-live posting is collected by the maintenance sweep,
	// so the join was only ever a safety net and it cost the whole scan.
	matchBandsSQL = `
		SELECT count(*) FILTER (WHERE band = 'strong'),
		       count(*) FILTER (WHERE band = 'plausible')
		  FROM user_job_scores
		 WHERE user_id = $1 AND band IN ('strong', 'plausible')`

	// Driven from the postings side, where freshness is indexed and the
	// population is small, then probed into the scores by primary key.
	matchNewTodaySQL = `
		SELECT count(*)
		  FROM job_postings p
		  JOIN user_job_scores s ON s.posting_id = p.id AND s.user_id = $1
		 WHERE p.status = 'live'
		   AND COALESCE(p.posted_at, p.first_seen_at) > now() - interval '24 hours'`

	// Genuinely proportional to the corpus, and there is no honest way to make
	// it cheaper than counting — an estimate would be a fabricated number on a
	// stat tile.
	matchScoredSQL = `SELECT count(*) FROM user_job_scores WHERE user_id = $1`

	pipelineCountsSQL = `
		SELECT status::text, count(*)
		  FROM applications WHERE user_id = $1
		 GROUP BY status`

	needsReplySQL = `
		SELECT id,
		       COALESCE(company_name, '')                                AS company_name,
		       COALESCE(role_title, '')                                  AS role_title,
		       status::text                                              AS status,
		       GREATEST(0, EXTRACT(day FROM now() - last_activity_at)::int) AS days_since,
		       COALESCE(next_action, '')                                 AS next_action,
		       next_action_at
		  FROM applications
		 WHERE user_id = $1
		   AND status IN ('applied','recruiter_screen','hm_screen','onsite')
		   AND (next_action_at <= current_date OR last_activity_at < now() - interval '7 days')
		 ORDER BY next_action_at NULLS LAST, last_activity_at
		 LIMIT 5`

	topMatchesSQL = `
		SELECT p.id,
		       p.title,
		       c.name                                        AS company_name,
		       COALESCE(p.location_raw, '')                  AS location,
		       COALESCE(p.mode::text, '')                    AS mode,
		       s.score,
		       s.band,
		       COALESCE(s.missing_skills, '{}')              AS missing_skills,
		       COALESCE(p.posted_at, p.first_seen_at)        AS posted_at,
		       COALESCE(p.apply_url, '')                     AS apply_url,
		       EXISTS (
		           SELECT 1 FROM applications a
		            WHERE a.user_id = $1 AND a.posting_id = p.id
		       )                                             AS saved
		  FROM user_job_scores s
		  JOIN job_postings p ON p.id = s.posting_id
		  JOIN companies c ON c.id = p.company_id
		 WHERE s.user_id = $1 AND p.status = 'live'
		 ORDER BY s.score DESC, p.first_seen_at DESC
		 LIMIT 6`
)

// LoadDashboard reads every dashboard widget in one batch.
//
// The widgets are independent, so one round trip per widget would be seven.
// pgx pipelines a batch over a single connection, which is why this costs one.
func LoadDashboard(ctx context.Context, pool *pgxpool.Pool, userID int64) (Dashboard, error) {
	batch := &pgx.Batch{}
	batch.Queue(matchBandsSQL, userID)
	batch.Queue(matchNewTodaySQL, userID)
	batch.Queue(matchScoredSQL, userID)
	batch.Queue(pipelineCountsSQL, userID)
	batch.Queue(needsReplySQL, userID)
	batch.Queue(topMatchesSQL, userID)
	batch.Queue(marketSummarySQL)

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	var d Dashboard
	var err error

	if err = br.QueryRow().Scan(&d.Matches.Strong, &d.Matches.Plausible); err != nil {
		return d, fmt.Errorf("match bands: %w", err)
	}
	if err = br.QueryRow().Scan(&d.Matches.NewToday); err != nil {
		return d, fmt.Errorf("new today: %w", err)
	}
	if err = br.QueryRow().Scan(&d.Matches.Scored); err != nil {
		return d, fmt.Errorf("scored total: %w", err)
	}
	if d.Pipeline, err = scanPipeline(br); err != nil {
		return d, err
	}
	if d.NeedsReply, err = collectBatch[ActionItem](br); err != nil {
		return d, fmt.Errorf("needs reply: %w", err)
	}
	if d.TopMatches, err = collectBatch[TopMatch](br); err != nil {
		return d, fmt.Errorf("top matches: %w", err)
	}
	if err = br.QueryRow().Scan(&d.Market.LivePostings, &d.Market.Companies,
		&d.Market.AddedThisWeek, &d.Market.RemoteShare); err != nil {
		return d, fmt.Errorf("market summary: %w", err)
	}
	return d, nil
}

// LoadMarketSummary reads the corpus figures alone, for the signed-out landing page.
func LoadMarketSummary(ctx context.Context, pool *pgxpool.Pool) (MarketSummary, error) {
	var m MarketSummary
	err := pool.QueryRow(ctx, marketSummarySQL).
		Scan(&m.LivePostings, &m.Companies, &m.AddedThisWeek, &m.RemoteShare)
	if err != nil {
		return m, fmt.Errorf("market summary: %w", err)
	}
	return m, nil
}

// collectBatch reads one batched query into a slice, mapping columns onto
// struct fields by name.
//
// By name and not by position: a column added to the SELECT without a matching
// struct field fails loudly here, where positional scanning would silently
// shift every subsequent value by one. That failure mode is the reason
// ADR-0015 chose this over hand-written Scan calls.
func collectBatch[T any](br pgx.BatchResults) ([]T, error) {
	rows, err := br.Query()
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scanPipeline(br pgx.BatchResults) (map[string]int, error) {
	rows, err := br.Query()
	if err != nil {
		return nil, fmt.Errorf("pipeline counts: %w", err)
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("pipeline counts: %w", err)
		}
		counts[status] = n
	}
	return counts, rows.Err()
}
