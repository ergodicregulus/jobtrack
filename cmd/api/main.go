// Command api serves the REST API.
package main

import (
	"context"

	"github.com/jobtrack/jobtrack/internal/api"
	"github.com/jobtrack/jobtrack/internal/app"
	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/jobs"
	"github.com/jobtrack/jobtrack/internal/migrate"
	"github.com/jobtrack/jobtrack/internal/normalise"
	"github.com/jobtrack/jobtrack/internal/version"
	"github.com/jobtrack/jobtrack/migrations"
)

func main() {
	// Before anything else, so a probe needs no configuration.
	httpx.RunHealthcheckIfAsked(":8080")

	ctx := context.Background()

	a, err := app.New(ctx, app.Options{Service: "api", NeedsDB: true, NeedsObject: true})
	if err != nil {
		app.Fatal(err)
	}

	// Refuse to serve against a schema this binary does not expect.
	//
	// The migrate Job should already have run, but a misconfigured rollout can
	// start pods first. Failing here — loudly, at startup — is far better than
	// failing later on a missing column, halfway through a user's request.
	m := migrate.New(a.Pool, migrations.FS, a.Log, version.Get().Version)
	if err := m.Verify(ctx); err != nil {
		a.Log.Error("refusing to start: schema check failed", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	// Insert-only River client: the API enqueues work, never performs it.
	// Started implicitly — an insert-only client needs no worker goroutines, so
	// there is nothing to start or stop.
	rc, err := jobs.New(ctx, &jobs.Deps{
		Pool:  a.Pool,
		Log:   a.Log,
		Cfg:   a.Cfg,
		Vocab: normalise.DefaultVocabulary(),
	}, jobs.RoleEnqueuer)
	if err != nil {
		a.Log.Error("could not build river client", "error", err)
		a.Close(ctx)
		app.Fatal(err)
	}

	svc, err := api.New(a.Cfg, a.Log, a.Pool, rc)
	if err != nil {
		app.Fatal(err)
	}
	srv := httpx.NewServer(httpx.ServerConfig{
		Addr:            a.Cfg.HTTPAddr,
		Handler:         svc.Routes(),
		Log:             a.Log,
		ShutdownTimeout: a.Cfg.ShutdownTimeout,
	})
	svc.SetServer(srv)

	if err := a.Run(srv.Run); err != nil {
		a.Log.Error("api stopped with error", "error", err)
		app.Fatal(err)
	}
}
