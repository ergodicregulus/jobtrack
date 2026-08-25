// Package api wires HTTP routes for the api service.
package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/jobtrack/jobtrack/internal/auth"
	"github.com/jobtrack/jobtrack/internal/config"
	"github.com/jobtrack/jobtrack/internal/crypt"
	"github.com/jobtrack/jobtrack/internal/httpx"
)

// API holds the dependencies every handler needs.
type API struct {
	cfg      *config.Config
	log      *slog.Logger
	pool     *pgxpool.Pool
	sessions *auth.SessionStore
	server   *httpx.Server

	// river inserts background work. Insert-only: the API runs no workers, so a
	// scoring batch can never land on a process that is serving a request.
	// Inserts go through the request transaction, which is what makes "profile
	// saved" and "rescore queued" a single atomic outcome.
	river *river.Client[pgx.Tx]

	// crypt seals resume text at rest. Nil is not tolerated: a misconfigured
	// key must stop the process at startup rather than write plaintext CVs.
	crypt *crypt.Cipher

	// parser is the isolated resume-parser service. Reached over HTTP because
	// it holds no database credentials by design — it cannot read or write
	// anything itself, so the API does the storing.
	parser *resumeClient
}

func New(cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, rc *river.Client[pgx.Tx]) (*API, error) {
	c, err := crypt.New(cfg.Security.ResumeKey)
	if err != nil {
		return nil, fmt.Errorf("resume encryption key: %w", err)
	}
	return &API{
		cfg:      cfg,
		log:      log,
		pool:     pool,
		sessions: auth.NewSessionStore(pool, cfg.Security.SessionTTL),
		river:    rc,
		crypt:    c,
		parser:   newResumeClient(cfg.ResumeParserURL),
	}, nil
}

// SetServer lets the readiness handler consult the server's drain state.
func (a *API) SetServer(s *httpx.Server) { a.server = s }

// Routes builds the handler tree.
//
// Go 1.22's ServeMux handles method matching and path wildcards, returns 405
// automatically when a path matches but the method does not, and detects
// conflicting patterns at registration time — i.e. at startup rather than on
// the request that hits the ambiguity. That covers everything we need, which is
// why there is no router dependency (ADR-0001).
func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()

	// Catch-all so an unmatched path returns problem+json like everything else.
	// Without it, ServeMux's default 404 is plain text, and a client that parses
	// every error as JSON breaks on exactly the response it is most likely to
	// hit during development.
	mux.Handle("/", httpx.Wrap(a.log, func(http.ResponseWriter, *http.Request) error {
		return httpx.ErrNotFound()
	}))

	// --- Operational endpoints: no auth, no rate limit, never logged ---
	mux.Handle("GET /healthz", httpx.Wrap(a.log, a.handleHealth))
	mux.Handle("GET /livez", httpx.Wrap(a.log, a.handleLive))
	mux.Handle("GET /readyz", httpx.Wrap(a.log, a.handleReady))

	// --- Public ---
	mux.Handle("POST /v1/auth/register", httpx.Wrap(a.log, a.handleRegister))
	mux.Handle("POST /v1/auth/login", httpx.Wrap(a.log, a.handleLogin))
	mux.Handle("POST /v1/auth/logout", httpx.Wrap(a.log, a.handleLogout))

	// The feed is public: discovery should not require an account. Scoring and
	// tracking do, which is the honest place to ask someone to sign up.
	maybeAuthed := a.optionalAuth()
	mux.Handle("GET /v1/jobs", maybeAuthed(httpx.Wrap(a.log, a.handleJobs)))
	mux.Handle("GET /v1/jobs/facets", httpx.Wrap(a.log, a.handleFacets))
	mux.Handle("GET /v1/market", httpx.Wrap(a.log, a.handleMarket))
	mux.Handle("GET /v1/market/ingest", httpx.Wrap(a.log, a.handleIngestSeries))
	mux.Handle("GET /v1/skills/common", httpx.Wrap(a.log, a.handleCommonSkills))
	// One posting in full, including its score breakdown. Optional auth for the
	// same reason as the feed: reading a posting needs no account, scoring does.
	mux.Handle("GET /v1/jobs/{id}", maybeAuthed(httpx.Wrap(a.log, a.handlePosting)))

	// --- Authenticated ---
	authed := a.requireAuth()
	mux.Handle("GET /v1/me", authed(httpx.Wrap(a.log, a.handleMe)))
	mux.Handle("GET /v1/me/preferences", authed(httpx.Wrap(a.log, a.handleGetPreferences)))
	mux.Handle("PATCH /v1/me/preferences", authed(httpx.Wrap(a.log, a.handlePatchPreferences)))
	mux.Handle("GET /v1/me/profile", authed(httpx.Wrap(a.log, a.handleGetProfile)))
	mux.Handle("PATCH /v1/me/profile", authed(httpx.Wrap(a.log, a.handlePatchProfile)))
	mux.Handle("POST /v1/me/onboarding/complete", authed(httpx.Wrap(a.log, a.handleCompleteOnboarding)))

	// Resume upload and review. Two steps on purpose: upload PROPOSES, apply
	// COMMITS. Nothing a parser inferred reaches a profile without the user
	// seeing it first.
	mux.Handle("POST /v1/me/resume", authed(httpx.Wrap(a.log, a.handleResumeUpload)))
	mux.Handle("POST /v1/me/resume/{id}/apply", authed(httpx.Wrap(a.log, a.handleResumeApply)))

	// Dashboard, saved jobs and application tracking.
	mux.Handle("GET /v1/me/dashboard", authed(httpx.Wrap(a.log, a.handleDashboard)))
	mux.Handle("GET /v1/me/activity", authed(httpx.Wrap(a.log, a.handleActivity)))
	mux.Handle("GET /v1/me/saved", authed(httpx.Wrap(a.log, a.handleListSaved)))
	mux.Handle("PUT /v1/me/saved/{id}", authed(httpx.Wrap(a.log, a.handleSaveJob)))
	mux.Handle("DELETE /v1/me/saved/{id}", authed(httpx.Wrap(a.log, a.handleUnsaveJob)))
	mux.Handle("PATCH /v1/me/saved/{id}", authed(httpx.Wrap(a.log, a.handleUpdateSaved)))

	// Auth-aware rate limiting sits inside the auth middleware so it can key on
	// user ID. The gateway already did the volumetric per-IP layer, where a
	// rejected request costs nothing because it never reached us.
	perIP := httpx.NewRateLimit(
		a.cfg.Security.RateLimitPerMinute,
		a.cfg.Security.RateLimitBurst,
		a.log,
		httpx.KeyByIP(a.cfg.Security.TrustedProxies),
	)

	return httpx.Chain(mux,
		httpx.Tracing(a.cfg.Service),
		httpx.RequestID(),
		httpx.Recover(a.log),
		httpx.SecurityHeaders(a.cfg.Env == "prod"),
		httpx.AccessLog(a.log),
		perIP.Middleware(),
		// 1 MiB default. Upload routes raise this explicitly; a global cap
		// means a route that forgets to think about body size is still bounded.
		httpx.MaxBodyBytes(1<<20),
		httpx.Timeout(10*time.Second),
	)
}
