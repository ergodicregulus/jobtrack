package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

// Server wraps http.Server with the shutdown sequence Kubernetes expects.
type Server struct {
	http *http.Server
	log  *slog.Logger

	// ready gates /readyz independently of /healthz. Flipping it false on
	// SIGTERM is what stops the load balancer sending new traffic *before* we
	// begin draining — get this order wrong and clients see connection resets.
	ready atomic.Bool

	// drainDelay covers endpoint propagation. Removal from the load balancer is
	// eventually consistent across kube-proxy and the gateway, so a pod that
	// stops accepting immediately will reject requests that were routed to it
	// microseconds earlier. This is the most commonly missed detail in
	// "zero-downtime" Kubernetes setups.
	drainDelay time.Duration
}

type ServerConfig struct {
	Addr            string
	Handler         http.Handler
	Log             *slog.Logger
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	DrainDelay      time.Duration
}

func NewServer(cfg ServerConfig) *Server {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 15 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		// Generous because SSE streams live here. Per-request bounds come from
		// the Timeout middleware, which is the right place for them.
		cfg.WriteTimeout = 120 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 90 * time.Second
	}
	if cfg.DrainDelay == 0 {
		cfg.DrainDelay = 5 * time.Second
	}
	// In development the delay is what a live-reload loop trips over: `air`
	// signals the old process and starts the new one immediately, so a 5s
	// drain means the replacement binds a port the predecessor still holds and
	// dies with "address already in use" — leaving a container that reports
	// running with no process inside it. The delay exists so a load balancer
	// can take a pod out of rotation, and there is no load balancer here.
	if cfg.DrainDelay > 250*time.Millisecond && os.Getenv("APP_ENV") == "dev" {
		cfg.DrainDelay = 250 * time.Millisecond
	}

	s := &Server{
		http: &http.Server{
			Addr:         cfg.Addr,
			Handler:      cfg.Handler,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			IdleTimeout:  cfg.IdleTimeout,
			// Bounds the slowloris attack surface: a client that opens a
			// connection and dribbles headers holds nothing for long.
			ReadHeaderTimeout: 5 * time.Second,
			ErrorLog:          slog.NewLogLogger(cfg.Log.Handler(), slog.LevelWarn),
		},
		log:        cfg.Log,
		drainDelay: cfg.DrainDelay,
	}
	s.ready.Store(true)
	return s
}

// Ready reports whether the server should receive traffic.
func (s *Server) Ready() bool { return s.ready.Load() }

// SetReady flips readiness. Used by the readiness probe and shutdown.
func (s *Server) SetReady(v bool) { s.ready.Store(v) }

// Run serves until ctx is cancelled, then drains.
//
// The sequence matters and is the whole reason this function exists rather than
// calling ListenAndServe directly:
//
//  1. fail readiness so the load balancer stops routing new requests
//  2. wait drainDelay for that removal to propagate
//  3. Shutdown: finish in-flight requests, refuse new ones
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		s.log.Info("http server listening", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err

	case <-ctx.Done():
		s.log.Info("draining http server", "drain_delay", s.drainDelay.String())
		s.SetReady(false)

		if s.drainDelay > 0 {
			time.Sleep(s.drainDelay)
		}

		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()

		if err := s.http.Shutdown(shutCtx); err != nil {
			s.log.Error("graceful shutdown failed; closing connections", "error", err)
			return s.http.Close()
		}
		s.log.Info("http server stopped cleanly")
		return nil
	}
}
