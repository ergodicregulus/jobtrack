package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/httpx"
	"github.com/ergodicregulus/jobtrack/internal/version"
)

// The three probes answer three different questions, and conflating them is the
// most common cause of self-inflicted rolling restarts:
//
//	/livez   Is the process wedged?      -> restart me
//	/readyz  Should I get traffic?       -> route to me
//	/healthz Human-readable status       -> for operators
//
// A liveness probe that checks the database will restart every pod when the
// database blips, turning a brief dependency outage into a full outage.

type healthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Commit  string            `json:"commit"`
	Checks  map[string]string `json:"checks,omitempty"`
}

// handleLive reports only that the process is running and can serve.
// It must never depend on anything external.
func (a *API) handleLive(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK,
		healthResponse{Status: "ok", Version: version.Get().Version})
	return nil
}

// handleReady gates traffic. It fails during drain and when a dependency the
// service cannot serve without is unavailable.
func (a *API) handleReady(w http.ResponseWriter, r *http.Request) error {
	// Draining: readiness flips false on SIGTERM before connections are closed,
	// so the load balancer stops sending new requests while in-flight ones
	// finish.
	if a.server != nil && !a.server.Ready() {
		httpx.WriteJSON(r.Context(), w, a.log, http.StatusServiceUnavailable,
			healthResponse{Status: "draining", Version: version.Get().Version})
		return nil
	}

	// A short, independent budget: readiness must answer quickly even when the
	// database is slow, or the probe itself times out and the pod is removed
	// for the wrong reason.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{}
	status := http.StatusOK

	if err := a.pool.Ping(ctx); err != nil {
		checks["database"] = "unreachable"
		status = http.StatusServiceUnavailable
		a.log.WarnContext(ctx, "readiness check failed", "check", "database", "error", err)
	} else {
		checks["database"] = "ok"
	}

	body := healthResponse{Status: "ok", Version: version.Get().Version, Checks: checks}
	if status != http.StatusOK {
		body.Status = "unavailable"
		w.Header().Set("Retry-After", "5")
	}
	httpx.WriteJSON(ctx, w, a.log, status, body)
	return nil
}

// handleHealth is for humans and dashboards.
func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) error {
	info := version.Get()
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: info.Version,
		Commit:  info.Commit,
	})
	return nil
}
