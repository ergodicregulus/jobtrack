package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/jobtrack/jobtrack/internal/store"
)

// Role selects which queues a process consumes.
//
// Every binary runs the same code with a different wiring — that is what makes
// six deployment units cheap. A process that only ENQUEUES (the API) takes no
// queues at all and therefore never competes for work.
type Role string

const (
	RoleIngestor  Role = "ingestor"
	RoleScheduler Role = "scheduler"
	RoleEnqueuer  Role = "enqueuer" // API: inserts jobs, works none
)

// New builds a River client for the given role.
// New builds the River client for one role.
//
// Deliberately takes no context: it registers workers and constructs a client,
// performing no I/O and honouring no cancellation. It carried a ctx parameter
// that was never used in the body, which promises a cancellation contract this
// function does not keep — a reader has to check to find that out.
func New(d *Deps, role Role) (*river.Client[pgx.Tx], error) {
	d.Init()

	queues, err := queuesFor(role)
	if err != nil {
		return nil, err
	}

	cfg := &river.Config{
		Queues:  queues,
		Workers: registerWorkers(d),
		Logger:  d.Log,
		// Retries back off and jitter. The default is sensible; the ceiling
		// matters because a vendor outage should not retry every 30s for hours.
		RetryPolicy: &river.DefaultClientRetryPolicy{},
		// Completed jobs are pruned rather than accumulating. Without this the
		// river_job table becomes the largest in the database.
		JobTimeout: 10 * time.Minute,
	}
	// The API is excluded because it takes no queues and does no maintenance.
	if role != RoleEnqueuer {
		cfg.PeriodicJobs = periodicJobs()
	}

	client, err := river.NewClient(riverpgxv5.New(d.Pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("create river client: %w", err)
	}
	d.River = client
	return client, nil
}

// registerWorkers builds the bundle. EVERY role registers EVERY worker; only
// the queue map decides what a process actually executes.
//
// This is not redundancy. River validates on insert that the job kind is present
// in the inserting client's worker bundle, so a process must know about every
// kind it *enqueues*, not merely the ones it *runs*. Registering per role looks
// tidier and is a trap: the ingestor enqueues score_posting inside the same
// transaction that writes the postings, so a missing registration failed that
// insert, rolled the transaction back, and threw away a completely successful
// fetch. Nothing was written, the job retried, and the symptom was an ingestion
// pipeline that ran forever at zero rows.
//
// Registration is free — it is a map entry, not a goroutine. Work only happens
// for queues named in queuesFor.
func registerWorkers(d *Deps) *river.Workers {
	workers := river.NewWorkers()
	river.AddWorker(workers, &FetchSourceWorker{Deps: d})
	river.AddWorker(workers, &DedupeCompanyWorker{Deps: d})
	river.AddWorker(workers, &ScheduleSourcesWorker{Deps: d})
	river.AddWorker(workers, &RetierSourcesWorker{Deps: d})
	river.AddWorker(workers, &PruneSessionsWorker{Deps: d})
	river.AddWorker(workers, &RetentionSweepWorker{Deps: d})
	river.AddWorker(workers, &RollupSourceDailyWorker{Deps: d})
	river.AddWorker(workers, &SendDigestsWorker{Deps: d})
	return workers
}

// queuesFor decides what this process actually runs.
func queuesFor(role Role) (map[string]river.QueueConfig, error) {
	queues := map[string]river.QueueConfig{}

	switch role {
	case RoleIngestor:
		// Fetching is network-bound, so concurrency far above core count is
		// correct here — the per-host limiter is what keeps it polite.
		queues[QueueIngest] = river.QueueConfig{MaxWorkers: 20}

	case RoleScheduler:
		queues[QueueMaint] = river.QueueConfig{MaxWorkers: 2}

	case RoleEnqueuer:
		// No queues: the API inserts work but never performs it. A user request
		// must never be slowed by a background job landing on the same process.

	default:
		return nil, fmt.Errorf("unknown river role %q", role)
	}
	return queues, nil
}

// periodicJobs is configured on EVERY worker role, not just the scheduler.
//
// The obvious design — put them only on the scheduler, which is a singleton —
// is wrong, and quietly so. River gates its periodic job enqueuer on ITS OWN
// leader election, which is global across every client connected to the database
// and is not something a particular process can be guaranteed to win.
// Configuring the list only on the scheduler means that whenever the ingestor
// happens to win the election, the leader runs an empty periodic list and
// NOTHING is ever enqueued.
//
// Observed exactly that: the scheduler logged "scheduler is leader" from its own
// advisory lock, held it correctly, and still enqueued nothing for twenty
// minutes, because a different client held River's leadership.
//
// Configuring the same list everywhere is both simpler and safe: River runs the
// enqueuer only on the leader, so the jobs still fire exactly once no matter
// which process wins. The double-enqueue this was trying to avoid is prevented
// by River's election, not by our process topology.
func periodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{
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
		// Daily, and NOT RunOnStart: nothing depends on it being prompt, and a
		// deploy loop would otherwise run a table sweep on every restart for no
		// benefit.
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return PruneSessionsArgs{}, nil },
			nil,
		),
		// Daily, and RunOnStart. A retention obligation that waits up to 24
		// hours after a deploy to first run is 24 hours of holding data past its
		// stated period, and the sweep is cheap enough that running it on every
		// start costs nothing.
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return RetentionSweepArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		// Weekly, and NOT RunOnStart. A deploy loop would otherwise email every
		// reader on every restart, which is the one failure here that cannot be
		// taken back.
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return SendDigestsArgs{Interval: 7 * 24 * time.Hour}, nil
			},
			nil,
		),
		// Hourly, not daily: today's row is incomplete until the day ends, and a
		// chart that only updates at midnight looks broken to anyone who checks
		// it during the day. RunOnStart so a fresh deploy has a populated chart
		// rather than an empty one.
		river.NewPeriodicJob(
			river.PeriodicInterval(1*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return RollupSourceDailyArgs{Days: 2}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}
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
type RollupSourceDailyWorker struct {
	river.WorkerDefaults[RollupSourceDailyArgs]
	Deps *Deps
}

func (w *RollupSourceDailyWorker) Work(ctx context.Context, job *river.Job[RollupSourceDailyArgs]) error {
	days := job.Args.Days
	if days <= 0 {
		// Two days, not one: a job running just after midnight would otherwise
		// leave yesterday half-counted, and recomputing a day that is already
		// correct costs nothing because the statement is idempotent.
		days = 2
	}
	rows, err := store.RebuildSourceDaily(ctx, w.Deps.Pool, days)
	if err != nil {
		return err
	}
	w.Deps.Log.InfoContext(ctx, "source rollup rebuilt", "days", days, "rows", rows)
	return nil
}

type RetentionSweepWorker struct {
	river.WorkerDefaults[RetentionSweepArgs]
	Deps *Deps
}

func (w *RetentionSweepWorker) Work(ctx context.Context, job *river.Job[RetentionSweepArgs]) error {
	res, err := store.RunRetentionSweep(ctx, w.Deps.Pool)
	if err != nil {
		return err
	}
	// Always logged, including a sweep that did nothing. "We ran it and there
	// was nothing to do" and "it never ran" look identical in a log that only
	// records deletions, and only one of them is compliance.
	w.Deps.Log.InfoContext(ctx, "retention sweep",
		"resumes_cleared", res.ResumesDeleted,
		"accounts_erased", res.AccountsErased)
	return nil
}

type PruneSessionsWorker struct {
	river.WorkerDefaults[PruneSessionsArgs]
	Deps *Deps
}

func (w *PruneSessionsWorker) Work(ctx context.Context, job *river.Job[PruneSessionsArgs]) error {
	deleted, err := store.DeleteExpiredSessions(ctx, w.Deps.Pool)
	if err != nil {
		return err
	}
	if deleted > 0 {
		w.Deps.Log.InfoContext(ctx, "expired sessions pruned", "rows", deleted)
	}
	return nil
}

type RetierSourcesWorker struct {
	river.WorkerDefaults[RetierSourcesArgs]
	Deps *Deps
}

func (w *RetierSourcesWorker) Work(ctx context.Context, job *river.Job[RetierSourcesArgs]) error {
	tag, err := store.RetierSources(ctx, w.Deps.Pool)
	if err != nil {
		return err
	}
	w.Deps.Log.InfoContext(ctx, "source tiers recomputed", "sources", tag)
	return nil
}
