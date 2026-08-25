// Package telemetry configures logging, tracing and metrics.
//
// Logging policy, because "accurate, no clutter" needs to be a rule rather than
// a preference:
//
//   - Log at decision boundaries, never at every frame. One line per outcome.
//   - Every log line carries trace_id, so a line is a pointer into a trace
//     rather than a substitute for one.
//   - Structured fields only. `slog.Int("source_id", id)`, never
//     fmt.Sprintf — one is queryable, the other is a grep.
//   - Never log PII. IDs only. Never a resume, email, or job description.
//   - Error means "a human must act". If nothing can be done, it is Warn.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/jobtrack/jobtrack/internal/version"
)

// Provider bundles everything that needs shutting down.
type Provider struct {
	Logger   *slog.Logger
	tracerFn func(context.Context) error
}

// Config is the subset of app config telemetry needs, kept narrow so this
// package does not import the whole config type.
type Config struct {
	Service      string
	Env          string
	LogLevel     string
	LogJSON      bool
	OTLPEndpoint string
	SampleRatio  float64
}

// Init wires logging and tracing. The returned Provider must be shut down.
func Init(ctx context.Context, cfg Config) (*Provider, error) {
	logger := newLogger(cfg)
	slog.SetDefault(logger)

	p := &Provider{Logger: logger}

	if cfg.OTLPEndpoint == "" {
		// No collector configured — valid for local development and for the
		// migrate job. Say so once rather than failing or silently dropping.
		logger.Debug("tracing disabled: no OTLP endpoint configured")
		p.tracerFn = func(context.Context) error { return nil }
		return p, nil
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	info := version.Get()
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("jobtrack-"+cfg.Service),
		semconv.ServiceVersion(info.Version),
		semconv.DeploymentEnvironment(cfg.Env),
	))
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		// ParentBased(TraceIDRatio) keeps a sampled trace sampled across every
		// service it touches. Sampling independently per service produces
		// traces with holes, which are worse than no traces.
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(cfg.SampleRatio),
		)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	p.tracerFn = tp.Shutdown
	logger.Info("tracing enabled", "endpoint", cfg.OTLPEndpoint, "sample_ratio", cfg.SampleRatio)
	return p, nil
}

func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tracerFn == nil {
		return nil
	}
	return p.tracerFn(ctx)
}

func newLogger(cfg Config) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(cfg.LogLevel),
		// Source is expensive and noisy. The trace tells you where you are far
		// better than a file:line does.
		AddSource: false,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Normalise the timestamp to RFC3339 with millisecond precision.
			// Consistent shape matters more than nanosecond fidelity.
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					return slog.String("ts", t.UTC().Format("2006-01-02T15:04:05.000Z"))
				}
			}
			if a.Key == slog.MessageKey {
				return slog.Attr{Key: "msg", Value: a.Value}
			}
			return a
		},
	}

	var h slog.Handler
	if cfg.LogJSON {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}

	// traceHandler injects trace correlation into every record.
	h = &traceHandler{Handler: h}

	info := version.Get()
	return slog.New(h).With(
		"service", cfg.Service,
		"env", cfg.Env,
		"version", info.Version,
	)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// traceHandler adds trace_id and span_id from the context.
//
// This is the single most valuable line-level field we emit: it turns every log
// line into a pointer into the corresponding trace, and it is why the API
// returns trace_id on errors — a user can paste it into a support message and
// we can find the exact request.
type traceHandler struct{ slog.Handler }

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithGroup(name)}
}

// Tracer returns the named tracer for this codebase.
func Tracer(name string) trace.Tracer {
	return otel.Tracer("github.com/jobtrack/jobtrack/" + name)
}

// TraceIDFromContext returns the current trace ID, or "" if untraced.
// Used by the HTTP error writer so every problem response is traceable.
func TraceIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// StartSpan is a thin wrapper so call sites do not each pick a tracer name.
