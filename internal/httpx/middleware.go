package httpx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/jobtrack/jobtrack/internal/telemetry"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware so that the first argument is the outermost layer.
//
// Go's stdlib mux has no route groups or middleware chaining, which is the one
// real cost of not taking a router dependency (ADR-0001). This is that cost, in
// full: about fifteen lines, explicit, and it never surprises anyone.
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyUserID
)

// RequestIDFromContext returns the per-request identifier.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// UserIDFromContext returns the authenticated user, or 0.
func UserIDFromContext(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxKeyUserID).(int64)
	return id
}

// WithUserID is used by the auth middleware.
func WithUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, id)
}

// Tracing starts a span per request and propagates incoming trace context.
//
// This runs outermost so that every subsequent middleware — including panic
// recovery and logging — has a trace ID available.
func Tracing(serviceName string) Middleware {
	tracer := telemetry.Tracer("httpx")
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Span name uses the *pattern*, not the path: "/v1/jobs/{id}" is one
			// operation, while "/v1/jobs/12345" would be a million of them and
			// would blow up trace cardinality.
			name := r.Method + " " + routePattern(r)
			ctx, span := tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			rw := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r.WithContext(ctx))

			span.SetAttributes(
				attrInt("http.response.status_code", rw.status),
			)
			if rw.status >= 500 {
				span.SetStatus(codesError, http.StatusText(rw.status))
			}
		})
	}
}

// RequestID assigns an identifier, preferring the trace ID so that logs, traces
// and the client-visible error field all agree on one value.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := telemetry.TraceIDFromContext(r.Context())
			if id == "" {
				id = randomHex(16)
			}
			ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Recover turns a panic into a 500 instead of a dropped connection.
//
// This is a safety net, not an error-handling strategy. A panic reaching here
// is a bug, so it is logged at Error with the stack.
func Recover(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// http.ErrAbortHandler is the documented way to abort a
					// response; it is not a bug and must not be logged as one.
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					log.ErrorContext(r.Context(), "panic recovered",
						"panic", rec,
						"method", r.Method,
						"path", r.URL.Path,
						"stack", stackTrace())
					WriteProblem(r.Context(), w, r, log, ErrInternal(errPanic))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// AccessLog writes exactly one line per request.
//
// One line, at the end, with the outcome. Not a "request started" line — that
// doubles log volume to tell you something the completion line already implies,
// and it is the single largest source of log clutter in most services.
func AccessLog(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health checks run every few seconds forever. Logging them buries
			// real traffic and costs money for zero information.
			if isHealthPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rw := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			// Level tracks who is at fault: 5xx needs a human, 4xx is the
			// caller's problem, 2xx is routine.
			level := slog.LevelInfo
			switch {
			case rw.status >= 500:
				level = slog.LevelError
			case rw.status >= 400:
				level = slog.LevelWarn
			}

			log.Log(r.Context(), level, "http request",
				"method", r.Method,
				"path", routePattern(r),
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", rw.written,
				// user_id, never email. IDs are not PII in our logs.
				"user_id", UserIDFromContext(r.Context()),
			)
		})
	}
}

// SecurityHeaders applies defence-in-depth headers to every response.
func SecurityHeaders(isProd bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			if isProd {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBodyBytes caps request bodies. Applied globally with a small limit;
// upload routes raise it explicitly.
func MaxBodyBytes(limit int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout bounds handler execution.
//
// Nested deadlines are the point: a request budget of 10s must not contain a
// 30s database call, so handlers derive their timeouts from this context rather
// than setting their own.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RateLimit is a per-key token bucket.
//
// Deliberately in-process: it is the second layer. The gateway does volumetric
// per-IP limiting, where a rejected request costs nothing because it never
// reaches us. This layer enforces per-user quotas, which need an identity the
// gateway does not resolve.
type RateLimit struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
	keyFn   func(*http.Request) string
	log     *slog.Logger
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimit(perMinute int, burst int, log *slog.Logger, keyFn func(*http.Request) string) *RateLimit {
	rl := &RateLimit{
		buckets: make(map[string]*bucket),
		rate:    float64(perMinute) / 60.0,
		burst:   float64(burst),
		keyFn:   keyFn,
		log:     log,
	}
	go rl.reap()
	return rl
}

// reap drops idle buckets. Without it the map grows without bound, which turns
// a rate limiter into a memory leak — a genuinely common production bug.
func (rl *RateLimit) reap() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if b.last.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimit) allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, last: now}
		return true
	}
	b.tokens = min(rl.burst, b.tokens+now.Sub(b.last).Seconds()*rl.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (rl *RateLimit) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := rl.keyFn(r)
			if key != "" && !rl.allow(key) {
				w.Header().Set("Retry-After", "60")
				WriteProblem(r.Context(), w, r, rl.log, ErrRateLimited("60 seconds"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// KeyByIP rate-limits on client address, honouring X-Forwarded-For only from
// trusted proxies.
//
// Two failure modes this has to avoid, and they pull in opposite directions:
//
//  1. Trusting X-Forwarded-For unconditionally makes the limiter trivially
//     bypassable — anyone can send a random value per request.
//
//  2. NOT trusting it collapses every user behind a proxy into ONE bucket.
//     That is not theoretical: our own SSR server calls this API on behalf of
//     every visitor, so without the forwarded address, one busy page would
//     rate-limit every other user of the site. A load test found exactly this.
//
// The resolution is an explicit allowlist: the forwarded address is honoured
// only when the immediate peer is a proxy we configured. Everything else keys
// on the socket address.
func KeyByIP(trustedProxies []string) func(*http.Request) string {
	trusted := make([]*net.IPNet, 0, len(trustedProxies))
	for _, p := range trustedProxies {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Accept both a bare address and a CIDR block, so a whole pod subnet
		// can be trusted without listing every replica.
		if !strings.Contains(p, "/") {
			if ip := net.ParseIP(p); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				trusted = append(trusted, &net.IPNet{
					IP: ip, Mask: net.CIDRMask(bits, bits),
				})
				continue
			}
		}
		if _, block, err := net.ParseCIDR(p); err == nil {
			trusted = append(trusted, block)
		}
	}

	isTrusted := func(host string) bool {
		ip := net.ParseIP(host)
		if ip == nil {
			return false
		}
		for _, block := range trusted {
			if block.Contains(ip) {
				return true
			}
		}
		return false
	}

	return func(r *http.Request) string {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !isTrusted(host) {
			return host
		}

		// Leftmost entry is the originating client. Everything after it was
		// appended by intermediaries.
		xff := r.Header.Get("X-Forwarded-For")
		if xff == "" {
			return host
		}
		if i := strings.IndexByte(xff, ','); i > 0 {
			xff = xff[:i]
		}
		client := strings.TrimSpace(xff)
		if net.ParseIP(client) == nil {
			// A malformed header from a trusted peer is a bug, not an attack.
			// Fall back rather than keying on garbage.
			return host
		}
		return client
	}
}

// KeyBySession rate-limits authenticated traffic per user.
func KeyBySession(r *http.Request) string {
	if id := UserIDFromContext(r.Context()); id != 0 {
		return "u:" + itoa(id)
	}
	return ""
}

// responseRecorder captures the status and byte count for logging and tracing.
type responseRecorder struct {
	http.ResponseWriter
	status  int
	written int
	wrote   bool
}

func (w *responseRecorder) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.written += n
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer, which is what
// keeps SSE flushing working through the middleware chain.
func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func isHealthPath(p string) bool {
	return p == "/healthz" || p == "/readyz" || p == "/livez"
}

// routePattern returns the matched pattern, falling back to the path.
// Using the pattern keeps log and metric cardinality bounded.
func routePattern(r *http.Request) string {
	if p := r.Pattern; p != "" {
		// Pattern is "GET /v1/jobs/{id}"; strip the method.
		if i := strings.IndexByte(p, ' '); i >= 0 {
			return p[i+1:]
		}
		return p
	}
	return r.URL.Path
}
