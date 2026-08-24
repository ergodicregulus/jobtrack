// Package app holds the wiring shared by every binary.
//
// Each cmd/*/main.go is deliberately thin: load config, build this, run one
// thing, shut down cleanly. Putting the shared bootstrap here rather than
// copying it six times is the difference between one graceful-shutdown bug and
// six of them.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/config"
	"github.com/jobtrack/jobtrack/internal/store"
	"github.com/jobtrack/jobtrack/internal/telemetry"
	"github.com/jobtrack/jobtrack/internal/version"
)

// App is the common runtime context for a service.
type App struct {
	Cfg  *config.Config
	Log  *slog.Logger
	Pool *pgxpool.Pool

	tel      *telemetry.Provider
	closers  []func(context.Context) error
	startCtx context.Context
}

// Options controls what a service needs. Not every binary needs a database
// (none currently), and asking keeps the failure surface honest.
type Options struct {
	Service     string
	NeedsDB     bool
	NeedsObject bool
}

// New loads config, starts telemetry, and connects dependencies.
func New(ctx context.Context, opts Options) (*App, error) {
	cfg, err := config.Load(opts.Service)
	if err != nil {
		// Config errors are printed before the logger exists, so use stderr
		// directly. The message already lists every problem at once.
		return nil, err
	}

	tel, err := telemetry.Init(ctx, telemetry.Config{
		Service:      cfg.Service,
		Env:          cfg.Env,
		LogLevel:     cfg.LogLevel,
		LogJSON:      cfg.LogJSON,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
		SampleRatio:  cfg.Telemetry.SampleRatio,
	})
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}

	a := &App{Cfg: cfg, Log: tel.Logger, tel: tel, startCtx: ctx}
	a.Log.Info("starting", "build", version.Get().String())

	if opts.NeedsDB {
		pool, err := store.Open(ctx, cfg.Database, a.Log)
		if err != nil {
			_ = tel.Shutdown(ctx)
			return nil, err
		}
		a.Pool = pool
		a.closers = append(a.closers, func(context.Context) error {
			pool.Close()
			return nil
		})
	}

	return a, nil
}

// Close releases resources in reverse order of acquisition.
func (a *App) Close(ctx context.Context) {
	for i := len(a.closers) - 1; i >= 0; i-- {
		if err := a.closers[i](ctx); err != nil {
			a.Log.Error("shutdown step failed", "error", err)
		}
	}
	if err := a.tel.Shutdown(ctx); err != nil {
		a.Log.Error("telemetry shutdown failed", "error", err)
	}
}

// Run executes fn with a context cancelled on SIGINT/SIGTERM, then waits up to
// ShutdownTimeout for it to finish.
//
// The ordering here is the whole point of graceful shutdown: the signal
// cancels the context, fn is expected to stop accepting new work and drain,
// and only then do we tear down connections. A process that closes its pool
// first turns every in-flight request into an error.
func (a *App) Run(fn func(ctx context.Context) error) error {
	ctx, stop := signal.NotifyContext(a.startCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- fn(ctx) }()

	select {
	case err := <-errCh:
		// Finished on its own — either completed work or failed.
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.Cfg.ShutdownTimeout)
		defer cancel()
		a.Close(shutCtx)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil

	case <-ctx.Done():
		a.Log.Info("signal received, draining", "timeout", a.Cfg.ShutdownTimeout.String())
		stop() // restore default handling: a second Ctrl-C kills immediately

		select {
		case err := <-errCh:
			shutCtx, cancel := context.WithTimeout(context.Background(), a.Cfg.ShutdownTimeout)
			defer cancel()
			a.Close(shutCtx)
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			a.Log.Info("stopped cleanly")
			return nil

		case <-time.After(a.Cfg.ShutdownTimeout):
			// Exceeding the grace period means Kubernetes is about to SIGKILL
			// us anyway. Say so loudly — a silent hard kill looks like a crash
			// in the logs and sends people hunting for the wrong bug.
			a.Log.Error("shutdown timed out; forcing exit",
				"timeout", a.Cfg.ShutdownTimeout.String())
			return errors.New("shutdown timed out")
		}
	}
}

// Fatal prints a startup error and exits non-zero. Used only from main, before
// a logger necessarily exists.
func Fatal(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
