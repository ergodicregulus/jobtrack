// Command scheduler enqueues periodic work and drives background data
// migrations.
//
// A singleton (replicas: 1) holding a Postgres advisory lock for leader
// election, so a rolling deploy that briefly overlaps two pods cannot
// double-enqueue. A cron loop inside the api service would fire once per
// replica — the bug this binary exists to prevent.
package main

import (
	"context"
	"errors"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/app"
	"github.com/ergodicregulus/jobtrack/internal/datamigrations"
	"github.com/ergodicregulus/jobtrack/internal/jobs"
	"github.com/ergodicregulus/jobtrack/internal/migrate"
	"github.com/ergodicregulus/jobtrack/internal/normalise"
	"github.com/ergodicregulus/jobtrack/internal/store"
	"github.com/ergodicregulus/jobtrack/internal/version"
	"github.com/ergodicregulus/jobtrack/migrations"
)

// leaderLockKey is distinct from the migration lock: holding one must not block
// the other.
const leaderLockKey int64 = 8_675_309_002

func main() {
	ctx := context.Background()

	a, err := app.New(ctx, app.Options{Service: "scheduler", NeedsDB: true})
	if err != nil {
		app.Fatal(err)
	}

	m := migrate.New(a.Pool, migrations.FS, a.Log, version.Get().Version)
	if err := m.Verify(ctx); err != nil {
		a.Log.Error("refusing to start: schema check failed", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	deps := &jobs.Deps{
		Pool:  a.Pool,
		Log:   a.Log,
		Cfg:   a.Cfg,
		Vocab: normalise.DefaultVocabulary(),
	}

	// River owns its own tables, so its migrator runs here rather than being
	// copied into migrations/. The scheduler is the singleton, so it is the
	// safe place to do it exactly once.
	if err := jobs.Migrate(ctx, deps); err != nil {
		a.Log.Error("river migration failed", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	client, err := jobs.New(deps, jobs.RoleScheduler)
	if err != nil {
		a.Log.Error("could not build river client", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	runner := migrate.NewDataRunner(a.Pool, a.Log, version.Get().Version, datamigrations.All()...)

	if err := a.RunWorker(func(ctx context.Context) error {
		// Hold the leader lock on a dedicated connection for the process
		// lifetime. try-lock rather than blocking: if another scheduler holds
		// it we exit rather than queue behind it, and Kubernetes restarts us
		// once the old pod is gone.
		conn, err := a.Pool.Acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()

		acquired, err := store.TryAdvisoryLock(ctx, conn, leaderLockKey)
		if err != nil {
			return err
		}
		if !acquired {
			a.Log.Warn("another scheduler holds the leader lock; exiting")
			return nil
		}
		a.Log.Info("scheduler is leader")

		if err := client.Start(ctx); err != nil {
			return err
		}

		// Data migrations are driven here rather than as a River periodic job:
		// they are resumable and long-running, and the runner already tracks
		// its own state, so wrapping it in a queue would add nothing.
		go runDataMigrations(ctx, a, runner)

		<-ctx.Done()
		return client.Stop(context.WithoutCancel(ctx))
	}); err != nil {
		a.Log.Error("scheduler stopped with error", "error", err)
		app.Fatal(err)
	}
}

func runDataMigrations(ctx context.Context, a *app.App, runner *migrate.DataRunner) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		if err := runner.RunPending(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.Log.ErrorContext(ctx, "data migrations failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
