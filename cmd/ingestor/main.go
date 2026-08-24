// Command ingestor fetches, normalises and deduplicates job postings.
//
// Scales on queue depth, not CPU: this workload is network-I/O bound, so it
// shows near-zero CPU while its backlog grows. A CPU-based autoscaler would
// scale it exactly backwards.
package main

import (
	"context"

	"github.com/jobtrack/jobtrack/internal/app"
	"github.com/jobtrack/jobtrack/internal/jobs"
	"github.com/jobtrack/jobtrack/internal/migrate"
	"github.com/jobtrack/jobtrack/internal/normalise"
	"github.com/jobtrack/jobtrack/internal/seed"
	"github.com/jobtrack/jobtrack/internal/store"
	"github.com/jobtrack/jobtrack/internal/version"
	"github.com/jobtrack/jobtrack/migrations"
)

func main() {
	ctx := context.Background()

	a, err := app.New(ctx, app.Options{Service: "ingestor", NeedsDB: true})
	if err != nil {
		app.Fatal(err)
	}

	m := migrate.New(a.Pool, migrations.FS, a.Log, version.Get().Version)
	if err := m.Verify(ctx); err != nil {
		a.Log.Error("refusing to start: schema check failed", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	vocab := normalise.DefaultVocabulary()

	// Reconcile the skills table with the vocabulary before any fetch runs.
	//
	// posting_skills joins `skills` on name, so a skill the extractor knows but
	// the table does not is silently discarded — no error, no log line, just
	// missing data. Doing this at startup means expanding the vocabulary takes
	// effect on the next deploy rather than on the next time someone remembers
	// to re-run the seed.
	if err := store.EnsureSkills(ctx, a.Pool, seed.SkillCategories()); err != nil {
		a.Log.Error("could not reconcile the skills table", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	deps := &jobs.Deps{
		Pool:  a.Pool,
		Log:   a.Log,
		Cfg:   a.Cfg,
		Vocab: vocab,
	}

	client, err := jobs.New(ctx, deps, jobs.RoleIngestor)
	if err != nil {
		a.Log.Error("could not build river client", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	if !a.Cfg.Ingest.Enabled {
		a.Log.Warn("ingestion disabled by configuration; workers will idle")
	}
	a.Log.Info("ingestor ready",
		"mode", a.Cfg.Ingest.Mode,
		"max_hosts", a.Cfg.Ingest.MaxHosts)

	if err := a.Run(func(ctx context.Context) error {
		if err := client.Start(ctx); err != nil {
			return err
		}
		<-ctx.Done()

		// Stop() lets in-flight jobs finish. Jobs are idempotent, so a hard
		// kill would be safe — but finishing cleanly avoids re-fetching a board
		// we already paid for, which is the polite thing to do to the vendor.
		return client.Stop(context.WithoutCancel(ctx))
	}); err != nil {
		a.Log.Error("ingestor stopped with error", "error", err)
		app.Fatal(err)
	}
}
