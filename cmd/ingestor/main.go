// Command ingestor fetches, normalises and deduplicates job postings.
//
// Scales on queue depth, not CPU: this workload is network-I/O bound, so it
// shows near-zero CPU while its backlog grows. A CPU-based autoscaler would
// scale it exactly backwards.
package main

import (
	"context"
	"fmt"

	"github.com/ergodicregulus/jobtrack/internal/app"
	"github.com/ergodicregulus/jobtrack/internal/httpx"
	"github.com/ergodicregulus/jobtrack/internal/jobs"
	"github.com/ergodicregulus/jobtrack/internal/migrate"
	"github.com/ergodicregulus/jobtrack/internal/normalise"
	"github.com/ergodicregulus/jobtrack/internal/seed"
	"github.com/ergodicregulus/jobtrack/internal/store"
	"github.com/ergodicregulus/jobtrack/internal/version"
	"github.com/ergodicregulus/jobtrack/migrations"
)

func main() {
	// `<binary> healthcheck` probes RunWorker's /readyz: the image is distroless,
	// so a container healthcheck has no curl and must be the binary itself.
	httpx.RunHealthcheckIfAsked(":8080")
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
	if err := reconcile(ctx, a); err != nil {
		a.Log.Error("could not reconcile product data", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	deps := &jobs.Deps{
		Pool:  a.Pool,
		Log:   a.Log,
		Cfg:   a.Cfg,
		Vocab: vocab,
	}

	client, err := jobs.New(deps, jobs.RoleIngestor)
	if err != nil {
		a.Log.Error("could not build river client", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	// It used to log this and then start the workers anyway, so the line
	// promised an idle process that went on fetching.
	if !a.Cfg.Ingest.Enabled {
		a.Log.Warn("ingestion disabled by configuration; this process will idle")
	}
	a.Log.Info("ingestor ready",
		"mode", a.Cfg.Ingest.Mode,
		"max_hosts", a.Cfg.Ingest.MaxHosts)

	if err := a.RunWorker(func(ctx context.Context) error {
		if !a.Cfg.Ingest.Enabled {
			<-ctx.Done() // still probe-able, still stops cleanly; fetches nothing
			return nil
		}
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

// reconcile makes the database match the product data that ships with this
// binary, before any fetch runs.
//
// The skills table first. posting_skills joins `skills` on name, so a skill the
// extractor knows but the table does not is silently discarded — no error, no
// log line, just missing data. Doing it at startup means expanding the
// vocabulary takes effect on the next deploy.
//
// Then the boards, for the same reason. Without this a production database has
// no sources at all: registration lived only in cmd/seed, which ships in no image
// and refuses to run against production.
func reconcile(ctx context.Context, a *app.App) error {
	if err := store.EnsureSkills(ctx, a.Pool, seed.SkillCategories()); err != nil {
		return fmt.Errorf("skills: %w", err)
	}
	registered, pruned, err := store.EnsureBoards(ctx, a.Pool, seed.Boards())
	if err != nil {
		return fmt.Errorf("boards: %w", err)
	}
	a.Log.Info("boards reconciled", "registered", registered, "pruned", pruned)
	return nil
}
