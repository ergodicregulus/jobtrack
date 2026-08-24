// Command matcher scores postings against user profiles.
//
// CPU-bound, and the reason it is a separate deployment: running this in-process
// with the API would move p99 latency every time a scoring batch lands.
package main

import (
	"context"

	"github.com/jobtrack/jobtrack/internal/app"
	"github.com/jobtrack/jobtrack/internal/jobs"
	"github.com/jobtrack/jobtrack/internal/migrate"
	"github.com/jobtrack/jobtrack/internal/normalise"
	"github.com/jobtrack/jobtrack/internal/version"
	"github.com/jobtrack/jobtrack/migrations"
)

func main() {
	ctx := context.Background()

	a, err := app.New(ctx, app.Options{Service: "matcher", NeedsDB: true})
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

	client, err := jobs.New(ctx, deps, jobs.RoleMatcher)
	if err != nil {
		a.Log.Error("could not build river client", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	a.Log.Info("matcher ready")

	if err := a.Run(func(ctx context.Context) error {
		if err := client.Start(ctx); err != nil {
			return err
		}
		<-ctx.Done()
		return client.Stop(context.WithoutCancel(ctx))
	}); err != nil {
		a.Log.Error("matcher stopped with error", "error", err)
		app.Fatal(err)
	}
}
