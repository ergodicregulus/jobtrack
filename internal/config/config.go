// Package config loads and validates configuration from the environment.
//
// Two rules, both learned the hard way:
//
//  1. Validation happens once at startup and reports *every* problem at once.
//     A service that starts successfully and fails an hour later on a missing
//     value is a service that pages someone at 3 a.m.
//  2. There are no silent defaults for anything that could differ between
//     environments. A default that is wrong in production is worse than a
//     startup failure that names the variable.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env     string // dev | staging | prod
	Service string // api | ingestor | matcher | scheduler | resume-parser | migrate

	// ResumeParserURL is where the api service reaches the isolated parser.
	// Only the api needs it; the parser itself does not call anyone.
	ResumeParserURL string
	LogLevel        string
	LogJSON         bool

	HTTPAddr        string
	ShutdownTimeout time.Duration

	Database Database
	Object   ObjectStore
	Telemetry
	Security
	Ingest
	Email
}

type Database struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	// StatementTimeout bounds every query. Set per-service because the API's
	// tolerance (fast or fail) differs from a worker's (slow is fine).
	StatementTimeout time.Duration
}

type ObjectStore struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

type Telemetry struct {
	OTLPEndpoint string
	SampleRatio  float64
}

type Security struct {
	SessionSecret string
	// ResumeKey encrypts resume text at rest.
	//
	// Separate from SessionSecret on purpose: a resume is the most sensitive
	// object we hold, and rotating a session secret (which logs everyone out,
	// and is therefore done casually) must not be able to make every stored CV
	// unreadable.
	ResumeKey      string
	SessionTTL     time.Duration
	CookieSecure   bool
	CookieDomain   string
	TrustedProxies []string

	// RateLimitPerMinute and RateLimitBurst bound requests per client IP.
	//
	// PER MINUTE, not per second — the previous hard-coded 100 read like a
	// generous per-second allowance and was in fact 1.7 requests a second.
	//
	// Configurable rather than hard-coded because the right value depends on
	// the deployment, not on the code. One page view costs several API calls,
	// so a figure tuned for a single browser is far too low anywhere clients
	// share an address: a CI runner, an office NAT, or the E2E suite, where
	// every browser lives in one container and collectively tripped a limit
	// meant for one person. That surfaced as "the job feed is unavailable
	// (429)", which looks like a broken feed rather than a working limiter.
	RateLimitPerMinute int
	RateLimitBurst     int
}

// Email is how digests leave the system.
//
// SMTP rather than a provider SDK, which is ADR-0019: it keeps the dependency
// count at zero — net/smtp is standard library — and makes changing provider a
// config change rather than a rewrite. Anything that speaks SMTP works,
// including something the operator already runs.
//
// Disabled by default. A deployment that has not configured a host sends
// nothing and says so at startup, rather than failing per-message later.
type Email struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	// From is the envelope and header sender.
	From string
	// BaseURL is where an unsubscribe link points. Wrong here means a dead link
	// in every digest, so it is required once email is enabled.
	BaseURL string
	// UnsubscribeSecret signs the unsubscribe token. Without it a link is
	// guessable and anyone can unsubscribe anyone.
	UnsubscribeSecret string
}

type Ingest struct {
	Enabled       bool
	Mode          string // fixture | recorded | live
	LiveAllowlist []string
	TierAInterval time.Duration
	TierBInterval time.Duration
	TierCInterval time.Duration
	MaxHosts      int
	UserAgent     string
}

// loader accumulates problems so Load can report all of them together.
type loader struct {
	problems []string
}

func (l *loader) str(key, def string, required bool) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		if required {
			l.problems = append(l.problems, key+" is required")
			return ""
		}
		return def
	}
	return v
}

func (l *loader) minLen(key string, n int, required bool) string {
	v := l.str(key, "", required)
	if v != "" && len(v) < n {
		l.problems = append(l.problems, fmt.Sprintf("%s must be at least %d characters", key, n))
	}
	return v
}

func (l *loader) intVal(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.problems = append(l.problems, fmt.Sprintf("%s must be an integer, got %q", key, v))
		return def
	}
	return n
}

func (l *loader) floatVal(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		l.problems = append(l.problems, fmt.Sprintf("%s must be a number, got %q", key, v))
		return def
	}
	return f
}

func (l *loader) boolVal(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.problems = append(l.problems, fmt.Sprintf("%s must be true or false, got %q", key, v))
		return def
	}
	return b
}

func (l *loader) dur(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.problems = append(l.problems, fmt.Sprintf("%s must be a duration like 30s or 2h, got %q", key, v))
		return def
	}
	return d
}

func (l *loader) list(key string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (l *loader) oneOf(key, def string, allowed ...string) string {
	v := l.str(key, def, false)
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	l.problems = append(l.problems,
		fmt.Sprintf("%s must be one of %s, got %q", key, strings.Join(allowed, ", "), v))
	return def
}

// Load reads configuration for the named service.
func Load(service string) (*Config, error) {
	l := &loader{}

	cfg := &Config{
		Env:             l.oneOf("APP_ENV", "dev", "dev", "staging", "prod"),
		Service:         service,
		ResumeParserURL: l.str("RESUME_PARSER_URL", "", service == "api"),
		LogLevel:        l.oneOf("LOG_LEVEL", "info", "debug", "info", "warn", "error"),
		LogJSON:         l.boolVal("LOG_JSON", false),
		HTTPAddr:        l.str("HTTP_ADDR", ":8080", false),
		ShutdownTimeout: l.dur("SHUTDOWN_TIMEOUT", 45*time.Second),

		Database: Database{
			// Required everywhere EXCEPT resume-parser, which must not be able
			// to reach Postgres at all — that isolation is the entire reason
			// the service exists (ADR-0007). Requiring it unconditionally meant
			// the parser crash-looped on startup behind `air`, which restarts
			// it and therefore hid the failure: the container reported running
			// while the binary had never once started.
			URL:              l.str("DATABASE_URL", "", service != "resume-parser"),
			MaxConns:         int32(l.intVal("DB_MAX_CONNS", defaultMaxConns(service))),
			MinConns:         int32(l.intVal("DB_MIN_CONNS", 1)),
			MaxConnLifetime:  l.dur("DB_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime:  l.dur("DB_MAX_CONN_IDLE", 30*time.Minute),
			StatementTimeout: l.dur("DB_STATEMENT_TIMEOUT", defaultStatementTimeout(service)),
		},

		Object: ObjectStore{
			Endpoint:  l.str("OBJECT_STORE_ENDPOINT", "", service == "api" || service == "resume-parser"),
			Bucket:    l.str("OBJECT_STORE_BUCKET", "jobtrack", false),
			AccessKey: l.str("OBJECT_STORE_ACCESS_KEY", "", false),
			SecretKey: l.str("OBJECT_STORE_SECRET_KEY", "", false),
			UseSSL:    l.boolVal("OBJECT_STORE_USE_SSL", false),
		},

		Telemetry: Telemetry{
			OTLPEndpoint: l.str("OTEL_EXPORTER_OTLP_ENDPOINT", "", false),
			SampleRatio:  l.floatVal("OTEL_SAMPLE_RATIO", 0.1),
		},

		Security: Security{
			// 32 bytes minimum: this key derives session identifiers.
			SessionSecret:      l.minLen("SESSION_SECRET", 32, service == "api"),
			ResumeKey:          l.minLen("RESUME_ENCRYPTION_KEY", 32, service == "api"),
			SessionTTL:         l.dur("SESSION_TTL", 30*24*time.Hour),
			CookieSecure:       l.boolVal("COOKIE_SECURE", true),
			CookieDomain:       l.str("COOKIE_DOMAIN", "", false),
			TrustedProxies:     l.list("TRUSTED_PROXIES"),
			RateLimitPerMinute: l.intVal("RATE_LIMIT_PER_MINUTE", 600),
			RateLimitBurst:     l.intVal("RATE_LIMIT_BURST", 60),
		},

		Email: Email{
			Enabled:           l.boolVal("EMAIL_ENABLED", false),
			Host:              l.str("EMAIL_SMTP_HOST", "", false),
			Port:              l.intVal("EMAIL_SMTP_PORT", 587),
			Username:          l.str("EMAIL_SMTP_USERNAME", "", false),
			Password:          l.str("EMAIL_SMTP_PASSWORD", "", false),
			From:              l.str("EMAIL_FROM", "", false),
			BaseURL:           l.str("EMAIL_BASE_URL", "", false),
			UnsubscribeSecret: l.str("EMAIL_UNSUBSCRIBE_SECRET", "", false),
		},
		Ingest: Ingest{
			Enabled:       l.boolVal("INGEST_ENABLED", true),
			Mode:          l.oneOf("INGEST_MODE", "fixture", "fixture", "recorded", "live"),
			LiveAllowlist: l.list("INGEST_LIVE_ALLOWLIST"),
			TierAInterval: l.dur("INGEST_TIER_A_INTERVAL", 2*time.Hour),
			TierBInterval: l.dur("INGEST_TIER_B_INTERVAL", 6*time.Hour),
			TierCInterval: l.dur("INGEST_TIER_C_INTERVAL", 24*time.Hour),
			MaxHosts:      l.intVal("INGEST_MAX_HOSTS", 10),
			UserAgent:     l.str("INGEST_USER_AGENT", "JobTrackBot/1.0 (+https://jobtrack.dev/bot)", false),
		},
	}

	cfg.validate(l)

	if len(l.problems) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(l.problems, "\n  - "))
	}
	return cfg, nil
}

// validate holds the cross-field rules — the ones a per-variable check cannot
// express, and which are exactly the dangerous ones.
func (c *Config) validate(l *loader) {
	if c.Database.URL != "" {
		if _, err := url.Parse(c.Database.URL); err != nil {
			l.problems = append(l.problems, "DATABASE_URL is not a valid URL")
		}
	}
	if c.Database.MaxConns < c.Database.MinConns {
		l.problems = append(l.problems, "DB_MAX_CONNS must be >= DB_MIN_CONNS")
	}

	// Live ingestion without an allowlist would point a dev machine at
	// thousands of real ATS endpoints. The guard belongs in code, not in a
	// wiki page nobody reads. See docs/architecture/adr/0004.
	if c.Ingest.Mode == "live" && len(c.Ingest.LiveAllowlist) == 0 {
		l.problems = append(l.problems,
			"INGEST_MODE=live requires INGEST_LIVE_ALLOWLIST (comma-separated source IDs)")
	}

	if c.Env == "prod" {
		if !c.Security.CookieSecure {
			l.problems = append(l.problems, "COOKIE_SECURE must be true when APP_ENV=prod")
		}
		if c.Telemetry.OTLPEndpoint == "" {
			l.problems = append(l.problems, "OTEL_EXPORTER_OTLP_ENDPOINT is required when APP_ENV=prod")
		}
		if strings.Contains(c.Database.URL, "sslmode=disable") {
			l.problems = append(l.problems, "DATABASE_URL must not use sslmode=disable when APP_ENV=prod")
		}
		if strings.HasPrefix(c.Security.SessionSecret, "dev-") {
			l.problems = append(l.problems, "SESSION_SECRET is still the development placeholder")
		}
	}
}

// defaultMaxConns encodes the per-service pool budget from
// docs/architecture/service-topology.md §5. Total across all services at max
// replicas must stay below the database's max_connections.
func defaultMaxConns(service string) int {
	switch service {
	case "api":
		return 10
	case "ingestor":
		return 5
	case "matcher":
		return 4
	case "scheduler":
		return 2
	case "migrate":
		return 2
	default:
		return 4
	}
}

// defaultStatementTimeout differs by service on purpose: a user-facing query
// that takes 30s should be killed, while a backfill batch legitimately might not.
func defaultStatementTimeout(service string) time.Duration {
	switch service {
	case "api":
		return 10 * time.Second
	case "migrate":
		return 0 // unbounded: index builds legitimately take a long time
	default:
		return 5 * time.Minute
	}
}

// ErrNotFound is returned by lookups that find nothing.
var ErrNotFound = errors.New("not found")
