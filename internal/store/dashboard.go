package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/matching"
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
	// Considered is how many postings were ranked to produce these counts.
	//
	// It replaces a "scored for you" total, which stopped meaning anything when
	// scores stopped being stored: every live posting is scorable now, so a
	// count of them is a fact about the corpus, not about the reader. This is
	// the honest figure — the size of the window the counts describe.
	Considered int
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
)

// LoadDashboard reads every dashboard widget in one batch.
//
// The widgets are independent, so one round trip per widget would be seven.
// pgx pipelines a batch over a single connection, which is why this costs one.
func LoadDashboard(
	ctx context.Context,
	pool *pgxpool.Pool,
	scorer *matching.Scorer,
	userID int64,
) (Dashboard, error) {
	batch := &pgx.Batch{}
	batch.Queue(pipelineCountsSQL, userID)
	batch.Queue(needsReplySQL, userID)
	batch.Queue(marketSummarySQL)

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	var d Dashboard
	var err error

	if d.Pipeline, err = scanPipeline(br); err != nil {
		return d, err
	}
	if d.NeedsReply, err = collectBatch[ActionItem](br); err != nil {
		return d, fmt.Errorf("needs reply: %w", err)
	}
	if err = br.QueryRow().Scan(&d.Market.LivePostings, &d.Market.Companies,
		&d.Market.AddedThisWeek, &d.Market.RemoteShare); err != nil {
		return d, fmt.Errorf("market summary: %w", err)
	}
	br.Close()

	// Matches are ranked here rather than counted from a table. One pass over
	// the bounded candidate set serves both the counts and the top six, so the
	// dashboard costs one ranking rather than four aggregates over a table that
	// no longer exists.
	//
	// The counts are over that window, not over the whole corpus, and the
	// interface says so. It is also the more useful claim: a strong match from
	// four months ago is not something a reader can act on today.
	if err := d.rankMatches(ctx, pool, scorer, userID); err != nil {
		return d, err
	}
	return d, nil
}

// rankMatches fills Matches and TopMatches from one scored candidate set.
func (d *Dashboard) rankMatches(
	ctx context.Context,
	pool *pgxpool.Pool,
	scorer *matching.Scorer,
	userID int64,
) error {
	profile, err := ProfileForScoring(ctx, pool, userID)
	if err != nil {
		return fmt.Errorf("load profile for ranking: %w", err)
	}

	ranked, err := rankCandidates(ctx, pool, scorer, FeedFilter{UserID: &userID}, profile)
	if err != nil {
		return err
	}

	d.Matches.Considered = len(ranked)
	for _, r := range ranked {
		switch r.match.Band {
		case "strong":
			d.Matches.Strong++
		case "plausible":
			d.Matches.Plausible++
		}
	}

	const topN = 6
	head := ranked
	if len(head) > topN {
		head = head[:topN]
	}
	items, err := feedItemsByID(ctx, pool, &userID, head)
	if err != nil {
		return err
	}
	d.TopMatches = make([]TopMatch, 0, len(items))
	for _, it := range items {
		d.TopMatches = append(d.TopMatches, TopMatch{
			ID: it.ID, Title: it.Title, CompanyName: it.CompanyName,
			Location: it.LocationRaw, Mode: it.Mode,
			Score: it.Match.Score, Band: it.Match.Band, Missing: it.Match.MissingSkills,
			PostedAt: it.scoring.PostedAt, ApplyURL: it.ApplyURL, Saved: it.Saved,
		})
	}
	return nil
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
