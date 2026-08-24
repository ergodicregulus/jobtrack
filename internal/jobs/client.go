package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/jobtrack/jobtrack/internal/matching"
)

// Role selects which queues a process consumes.
//
// Every binary runs the same code with a different wiring — that is what makes
// six deployment units cheap. A process that only ENQUEUES (the API) takes no
// queues at all and therefore never competes for work.
type Role string

const (
	RoleIngestor  Role = "ingestor"
	RoleMatcher   Role = "matcher"
	RoleScheduler Role = "scheduler"
	RoleEnqueuer  Role = "enqueuer" // API: inserts jobs, works none
)

// New builds a River client for the given role.
func New(ctx context.Context, d *Deps, role Role) (*river.Client[pgx.Tx], error) {
	d.Init()

	scorer := matching.NewScorer(matching.DefaultConfig(), matching.DefaultAdjacency())

	// EVERY role registers EVERY worker; only the queue map decides what a
	// process actually executes.
	//
	// This is not redundancy. River validates on insert that the job kind is
	// present in the inserting client's worker bundle, so a process must know
	// about every kind it *enqueues*, not merely the ones it *runs*. Registering
	// per role looks tidier and is a trap: the ingestor enqueues score_posting
	// inside the same transaction that writes the postings, so a missing
	// registration failed that insert, rolled the transaction back, and threw
	// away a completely successful fetch. Nothing was written, the job retried,
	// and the symptom was an ingestion pipeline that ran forever at zero rows.
	//
	// Registration is free — it is a map entry, not a goroutine. Work only
	// happens for queues named in cfg.Queues below.
	workers := river.NewWorkers()
	river.AddWorker(workers, &FetchSourceWorker{Deps: d})
	river.AddWorker(workers, &DedupeCompanyWorker{Deps: d})
	river.AddWorker(workers, &ScorePostingWorker{Deps: d, Scorer: scorer})
	river.AddWorker(workers, &ScoreUserWorker{Deps: d, Scorer: scorer})
	river.AddWorker(workers, &ScheduleSourcesWorker{Deps: d})
	river.AddWorker(workers, &RetierSourcesWorker{Deps: d})
	river.AddWorker(workers, &RescoreStaleWorker{Deps: d, Version: scorer.Version()})

	queues := map[string]river.QueueConfig{}

	switch role {
	case RoleIngestor:
		// Fetching is network-bound, so concurrency far above core count is
		// correct here — the per-host limiter is what keeps it polite.
		queues[QueueIngest] = river.QueueConfig{MaxWorkers: 20}

	case RoleMatcher:
		queues[QueueScore] = river.QueueConfig{MaxWorkers: 10}
		// Bulk gets far fewer workers on purpose: a full rescore must never
		// starve live scoring of newly-ingested postings.
		queues[QueueBulk] = river.QueueConfig{MaxWorkers: 2}

	case RoleScheduler:
		queues[QueueMaint] = river.QueueConfig{MaxWorkers: 2}

	case RoleEnqueuer:
		// No queues: the API inserts work but never performs it. A user request
		// must never be slowed by a background job landing on the same process.

	default:
		return nil, fmt.Errorf("unknown river role %q", role)
	}

	cfg := &river.Config{
		Queues:  queues,
		Workers: workers,
		Logger:  d.Log,
		// Retries back off and jitter. The default is sensible; the ceiling
		// matters because a vendor outage should not retry every 30s for hours.
		RetryPolicy: &river.DefaultClientRetryPolicy{},
		// Completed jobs are pruned rather than accumulating. Without this the
		// river_job table becomes the largest in the database.
		JobTimeout: 10 * time.Minute,
	}

	// Periodic jobs are configured on EVERY worker role, not just the scheduler.
	//
	// The obvious design — put them only on the scheduler, which is a singleton
	// — is wrong, and quietly so. River gates its periodic job enqueuer on ITS
	// OWN leader election, which is global across every client connected to the
	// database and is not something a particular process can be guaranteed to
	// win. Configuring the list only on the scheduler means that whenever the
	// ingestor or matcher happens to win the election, the leader runs an empty
	// periodic list and NOTHING is ever enqueued.
	//
	// Observed exactly that: the scheduler logged "scheduler is leader" from its
	// own advisory lock, held it correctly, and still enqueued nothing for
	// twenty minutes, because a different client held River's leadership.
	//
	// Configuring the same list everywhere is both simpler and safe: River runs
	// the enqueuer only on the leader, so the jobs still fire exactly once no
	// matter which process wins. The double-enqueue this was trying to avoid is
	// prevented by River's election, not by our process topology.
	//
	// The API is excluded because it takes no queues and does no maintenance.
	if role != RoleEnqueuer {
		cfg.PeriodicJobs = []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(1*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) {
					return ScheduleSourcesArgs{Limit: 200}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(1*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return RetierSourcesArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			// Rolls a scoring-algorithm change across the existing corpus.
			// RunOnStart so a deploy that bumps the version begins correcting
			// immediately rather than at the top of the next interval.
			river.NewPeriodicJob(
				river.PeriodicInterval(5*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) { return RescoreStaleArgs{Limit: 2000}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		}
	}

	client, err := river.NewClient(riverpgxv5.New(d.Pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("create river client: %w", err)
	}
	d.River = client
	return client, nil
}

// Migrate applies River's own schema.
//
// River owns its tables, so its migrations are separate from ours and run
// through its migrator rather than being copied into migrations/. Copying them
// would mean maintaining a fork of someone else's schema — and getting it
// subtly wrong on their next release.
func Migrate(ctx context.Context, d *Deps) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(d.Pool), &rivermigrate.Config{Logger: d.Log})
	if err != nil {
		return fmt.Errorf("create river migrator: %w", err)
	}

	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{})
	if err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	if len(res.Versions) > 0 {
		d.Log.Info("river schema migrated", "versions", len(res.Versions))
	}
	return nil
}

// RetierSourcesWorker recomputes polling tiers.
//
// Tier is driven by two things: observed change rate, and USER ATTENTION. The
// second is the elegant part — watching a company promotes its board to
// 2-hourly polling, so the sources that matter to real people are the fresh
// ones and the long tail costs almost nothing.
type RetierSourcesWorker struct {
	river.WorkerDefaults[RetierSourcesArgs]
	Deps *Deps
}

func (w *RetierSourcesWorker) Work(ctx context.Context, job *river.Job[RetierSourcesArgs]) error {
	tag, err := w.Deps.Pool.Exec(ctx, `
		UPDATE sources s
		   SET tier = CASE
		       -- Someone is watching this company: poll it every 2 hours.
		       WHEN EXISTS (SELECT 1 FROM watched_companies wc WHERE wc.company_id = s.company_id)
		            THEN 'a'::source_tier
		       WHEN s.last_changed_at > now() - interval '7 days'  THEN 'a'::source_tier
		       WHEN s.last_changed_at > now() - interval '30 days' THEN 'b'::source_tier
		       ELSE 'c'::source_tier
		   END,
		   updated_at = now()
		 WHERE s.tier <> 'paused'`)
	if err != nil {
		return fmt.Errorf("retier sources: %w", err)
	}
	w.Deps.Log.InfoContext(ctx, "source tiers recomputed", "sources", tag.RowsAffected())
	return nil
}

// RescoreStaleWorker re-enqueues scores produced by a superseded algorithm.
//
// Scores are denormalised, so a change to the scorer does not propagate on its
// own: every stored row keeps the number the old code produced until something
// recomputes it. Re-ingestion only touches postings whose content changed,
// which is precisely the wrong set — a scoring fix needs to reach the postings
// that did NOT change.
//
// Bounded and idempotent. Each sweep takes a slice of the outdated rows and
// enqueues normal scoring jobs for them, so a version bump drains over hours
// through the same path as everything else rather than as a special case.
type RescoreStaleWorker struct {
	river.WorkerDefaults[RescoreStaleArgs]
	Deps *Deps
	// Version is the scorer's current version, injected rather than read
	// globally so a test can drive the sweep without a running scorer.
	Version string
}

func (w *RescoreStaleWorker) Work(ctx context.Context, job *river.Job[RescoreStaleArgs]) error {
	limit := job.Args.Limit
	if limit <= 0 || limit > 10_000 {
		limit = 2000
	}

	// DISTINCT posting_id: one scoring job re-scores a posting for every user it
	// suits, so enqueuing per (user, posting) row would multiply the work by the
	// user count for no benefit.
	rows, err := w.Deps.Pool.Query(ctx, `
		SELECT DISTINCT s.posting_id
		  FROM user_job_scores s
		  JOIN job_postings p ON p.id = s.posting_id
		 WHERE s.profile_version <> $1
		   AND p.status = 'live'
		 LIMIT $2`, w.Version, limit)
	if err != nil {
		return fmt.Errorf("find stale scores: %w", err)
	}

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Collect scores for postings that are no longer live, in the same sweep.
	//
	// Nothing else ever removes them: this worker skips non-live rows by
	// design, and the per-posting delete only fires when a posting is
	// RE-scored, which a superseded one never is. The result was 38,973 rows
	// unreachable from every query — harmless today, unbounded over time, and
	// the kind of thing that is free to fix while someone is in this file and
	// expensive to fix during an incident.
	//
	// Bounded like everything else here: a delete that takes a lock
	// proportional to corpus size is an outage waiting for a big enough corpus.
	tag, err := w.Deps.Pool.Exec(ctx, `
		DELETE FROM user_job_scores s
		 WHERE s.posting_id IN (
		   SELECT id FROM job_postings
		    WHERE status <> 'live'
		    LIMIT $1
		 )`, limit)
	if err != nil {
		return fmt.Errorf("collect scores for closed postings: %w", err)
	}
	if n := tag.RowsAffected(); n > 0 {
		w.Deps.Log.InfoContext(ctx, "scores collected for closed postings", "rows", n)
	}

	if len(ids) == 0 {
		return nil
	}

	// Onto the BULK queue, overriding ScorePostingArgs' own default.
	//
	// QueueBulk exists so that "a full rescore must never starve live scoring",
	// and this sweep is the largest producer of rescores in the system — a
	// version bump enqueues the entire corpus. Taking the args' default sent
	// every one of those to QueueScore, the ten-worker queue reserved for
	// scoring postings as they arrive, which is precisely the starvation the
	// split was built to prevent. Observed: a 2,200-posting ingest backed up
	// 3,553 jobs on the live queue and took interactive dashboard latency from
	// ~4s to ~15s while it drained.
	//
	// A newly-ingested posting still goes to QueueScore, because that one IS
	// time-sensitive: it is the difference between a posting appearing in the
	// feed now and in an hour.
	bulk := &river.InsertOpts{Queue: QueueBulk}
	params := make([]river.InsertManyParams, 0, len(ids))
	for _, id := range ids {
		params = append(params, river.InsertManyParams{
			Args:       ScorePostingArgs{PostingID: id},
			InsertOpts: bulk,
		})
	}
	if _, err := w.Deps.River.InsertMany(ctx, params); err != nil {
		return fmt.Errorf("enqueue rescore: %w", err)
	}

	w.Deps.Log.InfoContext(ctx, "stale scores re-enqueued",
		"count", len(ids), "current_version", w.Version)
	return nil
}
