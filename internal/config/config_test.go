package config

import (
	"os"
	"strings"
	"testing"
)

// The point of this package is to fail at startup rather than an hour later, so
// the tests are all about what it *rejects*. A config loader that accepts
// everything is worse than none: it converts a clear boot failure into a 3 a.m.
// page.

// namespaces is every environment prefix this package reads.
//
// The test process inherits the environment, and `make test` runs inside the
// compose `tools` container, which sets DATABASE_URL, RESUME_ENCRYPTION_KEY
// and the rest from the same anchor the services use. A test whose subject is
// *which variables are required* must therefore not read any of them from the
// ambient environment: doing so passed in the container and failed on a clean
// host for eight months, which is the worst possible direction for a config
// test to be wrong in.
var namespaces = []string{
	"APP_", "COOKIE_", "DATABASE_", "DB_", "EMAIL_", "HTTP_", "INGEST_",
	"LOG_", "OTEL_", "RATE_", "RESUME_", "SESSION_", "SHUTDOWN_",
	"TRUSTED_",
}

// setEnv installs kv as the ENTIRE JobTrack environment for this test.
//
// Blanking rather than unsetting: every loader accessor treats an empty value
// as absent (see loader.str), and t.Setenv restores on cleanup while
// os.Unsetenv would need its own. The file already relied on this — one test
// asks for DATABASE_URL="" and expects "is required".
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, entry := range os.Environ() {
		k, _, _ := strings.Cut(entry, "=")
		for _, ns := range namespaces {
			if strings.HasPrefix(k, ns) {
				t.Setenv(k, "")
				break
			}
		}
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

// The minimum that must be present for a service to load at all. The two
// RESUME_ values are required for the api service specifically; the other
// services ignore them, so one fixture still serves every case.
func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":          "postgres://u:p@localhost:5432/db?sslmode=disable",
		"SESSION_SECRET":        "0123456789abcdef0123456789abcdef",
		"RESUME_PARSER_URL":     "http://localhost:9090",
		"RESUME_ENCRYPTION_KEY": "fedcba9876543210fedcba9876543210",
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := Load("api")
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if cfg.Service != "api" {
		t.Errorf("Service = %q, want api", cfg.Service)
	}
	if cfg.Env != "dev" {
		t.Errorf("Env = %q, want dev by default", cfg.Env)
	}
	// The per-service connection budget must actually be applied, not just
	// documented — it is the contract that stops one service starving another.
	if cfg.Database.MaxConns != 10 {
		t.Errorf("api DB_MAX_CONNS = %d, want 10 (service-topology §5)", cfg.Database.MaxConns)
	}
}

func TestLoad_PerServiceConnectionBudget(t *testing.T) {
	want := map[string]int32{
		"api": 10, "ingestor": 5, "matcher": 4, "scheduler": 2, "migrate": 2,
	}
	for service, expected := range want {
		t.Run(service, func(t *testing.T) {
			setEnv(t, validEnv())
			cfg, err := Load(service)
			if err != nil {
				t.Fatalf("load %s: %v", service, err)
			}
			if cfg.Database.MaxConns != expected {
				t.Errorf("%s MaxConns = %d, want %d", service, cfg.Database.MaxConns, expected)
			}
		})
	}
}

// Every problem must be reported at once. Reporting them one at a time means a
// developer restarts the service five times to fix five variables.
func TestLoad_ReportsAllProblemsAtOnce(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL":   "",
		"SESSION_SECRET": "too-short",
		"LOG_LEVEL":      "verbose",
	})

	_, err := Load("api")
	if err == nil {
		t.Fatal("invalid config was accepted")
	}
	msg := err.Error()

	for _, want := range []string{"DATABASE_URL", "SESSION_SECRET", "LOG_LEVEL"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not mention %s; all problems must be reported at once.\ngot: %s",
				want, msg)
		}
	}
}

// Live ingestion without an allowlist would point a developer's machine at
// thousands of real ATS endpoints. The guard belongs in code, not a wiki page.
func TestLoad_LiveIngestRequiresAllowlist(t *testing.T) {
	t.Run("live without allowlist is rejected", func(t *testing.T) {
		env := validEnv()
		env["INGEST_MODE"] = "live"
		setEnv(t, env)

		_, err := Load("ingestor")
		if err == nil {
			t.Fatal("INGEST_MODE=live without an allowlist was accepted")
		}
		if !strings.Contains(err.Error(), "INGEST_LIVE_ALLOWLIST") {
			t.Errorf("error should name INGEST_LIVE_ALLOWLIST, got: %v", err)
		}
	})

	t.Run("live with allowlist is accepted", func(t *testing.T) {
		env := validEnv()
		env["INGEST_MODE"] = "live"
		env["INGEST_LIVE_ALLOWLIST"] = "1,2,3"
		setEnv(t, env)

		cfg, err := Load("ingestor")
		if err != nil {
			t.Fatalf("live with allowlist rejected: %v", err)
		}
		if len(cfg.Ingest.LiveAllowlist) != 3 {
			t.Errorf("allowlist = %v, want 3 entries", cfg.Ingest.LiveAllowlist)
		}
	})

	t.Run("fixture mode is the default", func(t *testing.T) {
		setEnv(t, validEnv())
		cfg, err := Load("ingestor")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Ingest.Mode != "fixture" {
			t.Errorf("default INGEST_MODE = %q, want fixture — live must never be the default",
				cfg.Ingest.Mode)
		}
	})
}

// Production has stricter rules, and each of these has burned somebody.
func TestLoad_ProductionGuards(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{
			"APP_ENV":                     "prod",
			"DATABASE_URL":                "postgres://u:p@db:5432/jobtrack?sslmode=require",
			"SESSION_SECRET":              "0123456789abcdef0123456789abcdef",
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318",
			"COOKIE_SECURE":               "true",
			"RESUME_PARSER_URL":           "http://resume-parser:9090",
			"RESUME_ENCRYPTION_KEY":       "fedcba9876543210fedcba9876543210",
		}
	}

	t.Run("valid production config", func(t *testing.T) {
		setEnv(t, base())
		if _, err := Load("api"); err != nil {
			t.Fatalf("valid prod config rejected: %v", err)
		}
	})

	cases := map[string]struct{ key, value, wantIn string }{
		"insecure cookies":       {"COOKIE_SECURE", "false", "COOKIE_SECURE"},
		"no telemetry endpoint":  {"OTEL_EXPORTER_OTLP_ENDPOINT", "", "OTEL_EXPORTER_OTLP_ENDPOINT"},
		"sslmode=disable":        {"DATABASE_URL", "postgres://u:p@db:5432/j?sslmode=disable", "sslmode=disable"},
		"dev placeholder secret": {"SESSION_SECRET", "dev-only-not-a-real-secret-min-32", "placeholder"},
		"dev placeholder resume key": {
			"RESUME_ENCRYPTION_KEY", "dev-only-resume-key-also-min-32-chars", "RESUME_ENCRYPTION_KEY"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := base()
			env[tc.key] = tc.value
			setEnv(t, env)

			_, err := Load("api")
			if err == nil {
				t.Fatalf("production accepted %s=%q", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error should mention %q, got: %v", tc.wantIn, err)
			}
		})
	}
}

func TestLoad_RejectsMalformedValues(t *testing.T) {
	cases := map[string]struct{ key, value string }{
		"bad duration": {"SESSION_TTL", "forever"},
		"bad integer":  {"DB_MAX_CONNS", "many"},
		"bad boolean":  {"LOG_JSON", "yes-please"},
		"bad enum":     {"APP_ENV", "production"}, // must be "prod"
		"bad float":    {"OTEL_SAMPLE_RATIO", "half"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			env[tc.key] = tc.value
			setEnv(t, env)

			_, err := Load("api")
			if err == nil {
				t.Fatalf("accepted %s=%q", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("error should name the offending variable %s, got: %v", tc.key, err)
			}
		})
	}
}

func TestLoad_MaxConnsMustExceedMinConns(t *testing.T) {
	env := validEnv()
	env["DB_MAX_CONNS"] = "2"
	env["DB_MIN_CONNS"] = "10"
	setEnv(t, env)

	if _, err := Load("api"); err == nil {
		t.Fatal("accepted DB_MAX_CONNS < DB_MIN_CONNS, which deadlocks the pool at startup")
	}
}

// migrate must not have a statement timeout: an index build legitimately takes
// longer than any request-shaped bound.
func TestLoad_MigrateHasNoStatementTimeout(t *testing.T) {
	setEnv(t, validEnv())
	cfg, err := Load("migrate")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.StatementTimeout != 0 {
		t.Errorf("migrate StatementTimeout = %v, want 0 (unbounded); "+
			"a timeout here would kill long index builds mid-deploy",
			cfg.Database.StatementTimeout)
	}
}
