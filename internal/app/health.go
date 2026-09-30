package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/httpx"
)

// RunWorker is Run for a service with no HTTP surface of its own — the
// ingestor and the scheduler. Alongside fn it serves /livez and /readyz on
// HTTPAddr, so a probe can tell a working worker from a dead or wedged one.
//
// Without it they were invisible. On 2026-09-30 a new migration made both
// binaries refuse to start ("database is behind the binary"); under `air` the
// containers stayed up with no process inside, compose reported them running,
// and a live corpus rebuild sat stalled with 45 fetches queued and nothing
// working them. deploy/k8s/workers.yaml had named the same gap for production: a
// wedged worker is never restarted, because there was nothing to probe.
//
// If the health server cannot bind, fn is cancelled and Run returns the error:
// a worker that cannot be probed must not keep running unobserved.
func (a *App) RunWorker(fn func(ctx context.Context) error) error {
	return a.Run(func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		health := make(chan error, 1)
		go func() {
			err := a.serveHealth(ctx)
			if err != nil {
				cancel()
			}
			health <- err
		}()

		err := fn(ctx)
		cancel()
		healthErr := <-health
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return healthErr
	})
}

func (a *App) serveHealth(ctx context.Context) error {
	mux := http.NewServeMux()
	// Liveness: the process is up and its goroutines are being scheduled. The
	// same path the api probes, so every service's probes look alike.
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	// Readiness: it can reach the database River keeps its queue in. A worker
	// that cannot is doing nothing, whatever its process table says.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if a.Pool == nil {
			http.Error(w, "no database configured", http.StatusServiceUnavailable)
			return
		}
		pctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := a.Pool.Ping(pctx); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	return httpx.NewServer(httpx.ServerConfig{
		Addr:    a.Cfg.HTTPAddr,
		Handler: mux,
		Log:     a.Log,
		// The drain delay exists so a load balancer can stop routing to a pod.
		// Nothing routes to a worker, so there is nothing to wait for.
		DrainDelay: time.Millisecond,
	}).Run(ctx)
}
