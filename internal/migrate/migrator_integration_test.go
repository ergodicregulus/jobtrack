//go:build integration

package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests run against a real Postgres because the behaviour under test is
// entirely about Postgres semantics: advisory locks, transactional DDL, and
// what happens when two migrators race. A mock would test the mock.
//
//	make test-integration     (or)     go test -tags=integration ./internal/migrate/

func adminURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	return url
}

// newTestDB creates an isolated database per test so tests cannot interfere,
// and drops it afterwards.
func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	base := adminURL(t)

	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close()

	name := fmt.Sprintf("jt_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	url := replaceDBName(base, name)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		a, err := pgxpool.New(context.Background(), base)
		if err != nil {
			return
		}
		defer a.Close()
		_, _ = a.Exec(context.Background(),
			"DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	return pool
}

func replaceDBName(url, name string) string {
	// postgres://user:pass@host:port/dbname?params
	i := strings.LastIndex(url, "/")
	rest := ""
	if q := strings.Index(url[i:], "?"); q >= 0 {
		rest = url[i+q:]
	}
	return url[:i+1] + name + rest
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// twoMigrations is a minimal filesystem standing in for migrations/.
// Using a synthetic set rather than the real one keeps these tests focused on
// migrator behaviour and independent of schema churn.
func twoMigrations() fs.FS {
	return fstest.MapFS{
		"0001_first.up.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
		"0002_second.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")},
	}
}

func threeMigrations() fs.FS {
	return fstest.MapFS{
		"0001_first.up.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
		"0002_second.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")},
		"0003_third.up.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE c (id int);")},
	}
}

// The central requirement: a fresh database runs everything, and an upgrade
// runs only what was added since. Both go through the same code path.
func TestUp_FreshInstallThenUpgradeAppliesOnlyNew(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	log := testLogger()

	// --- Fresh install: release 1.0.0 with two migrations ---
	v1 := New(pool, twoMigrations(), log, "1.0.0")

	plan, err := v1.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !plan.IsFreshDatabase {
		t.Fatal("expected a fresh database")
	}
	if len(plan.Pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(plan.Pending))
	}

	if err := v1.Up(ctx, 30*time.Second); err != nil {
		t.Fatalf("fresh up: %v", err)
	}
	assertTablesExist(t, pool, "a", "b")

	// --- Upgrade: release 1.0.1 adds a third migration ---
	v2 := New(pool, threeMigrations(), log, "1.0.1")

	plan, err = v2.Status(ctx)
	if err != nil {
		t.Fatalf("status after upgrade load: %v", err)
	}
	if plan.IsFreshDatabase {
		t.Error("database should not look fresh after the first migration run")
	}
	if plan.Applied != 2 {
		t.Errorf("applied = %d, want 2", plan.Applied)
	}
	if len(plan.Pending) != 1 {
		t.Fatalf("pending = %d, want exactly 1 (only the newly added migration)", len(plan.Pending))
	}
	if plan.Pending[0].Version != 3 {
		t.Errorf("pending version = %d, want 3", plan.Pending[0].Version)
	}

	if err := v2.Up(ctx, 30*time.Second); err != nil {
		t.Fatalf("upgrade up: %v", err)
	}
	assertTablesExist(t, pool, "a", "b", "c")

	// The ledger must attribute each migration to the release that applied it,
	// which is what makes "when did this column appear?" answerable.
	var appVersion string
	if err := pool.QueryRow(ctx,
		`SELECT app_version FROM schema_migrations WHERE version = 3`).Scan(&appVersion); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if appVersion != "1.0.1" {
		t.Errorf("migration 3 attributed to %q, want 1.0.1", appVersion)
	}
}

// Re-running with no new migrations must be a no-op, because that is what
// happens on every redeploy that did not change the schema.
func TestUp_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	m := New(pool, twoMigrations(), testLogger(), "1.0.0")

	if err := m.Up(ctx, 30*time.Second); err != nil {
		t.Fatalf("first up: %v", err)
	}
	if err := m.Up(ctx, 30*time.Second); err != nil {
		t.Fatalf("second up should be a no-op, got: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("ledger has %d rows after two runs, want 2", count)
	}
}

// Editing an applied migration produces databases that agree on version numbers
// and disagree on schema. It must fail loudly rather than be ignored.
func TestUp_DetectsEditedMigration(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	log := testLogger()

	if err := New(pool, twoMigrations(), log, "1.0.0").Up(ctx, 30*time.Second); err != nil {
		t.Fatalf("initial up: %v", err)
	}

	tampered := fstest.MapFS{
		"0001_first.up.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE a (id int, extra text);")},
		"0002_second.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")},
	}

	err := New(pool, tampered, log, "1.0.1").Up(ctx, 30*time.Second)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("editing an applied migration should fail with ErrChecksumMismatch, got: %v", err)
	}
}

// Verify is the readiness gate: an instance whose binary expects a schema the
// database does not have must refuse to serve.
func TestVerify_FailsWhenDatabaseIsBehind(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	log := testLogger()

	if err := New(pool, twoMigrations(), log, "1.0.0").Up(ctx, 30*time.Second); err != nil {
		t.Fatalf("up: %v", err)
	}

	// A newer binary against the older database.
	if err := New(pool, threeMigrations(), log, "1.0.1").Verify(ctx); err == nil {
		t.Fatal("Verify should fail when the database is behind the binary")
	}
	// The same binary that migrated it must pass.
	if err := New(pool, twoMigrations(), log, "1.0.0").Verify(ctx); err != nil {
		t.Fatalf("Verify should pass when up to date, got: %v", err)
	}
}

// Every parallel deploy starts several migrators at once. Exactly one must do
// the work; the others must wait and then find nothing to do.
func TestUp_ConcurrentMigratorsAreSerialised(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	log := testLogger()

	const racers = 5
	var wg sync.WaitGroup
	errs := make([]error, racers)

	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := New(pool, twoMigrations(), log, "1.0.0")
			errs[i] = m.Up(ctx, 60*time.Second)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("migrator %d failed: %v", i, err)
		}
	}

	// Each migration must appear exactly once. A duplicate key error here would
	// mean two migrators applied the same migration.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("ledger has %d rows after %d concurrent migrators, want 2", count, racers)
	}
}

// A duplicate version number means two developers branched and both took the
// same slot. Catching it at load time is much cheaper than at deploy time.
func TestLoad_RejectsDuplicateVersions(t *testing.T) {
	pool := newTestDB(t)
	dup := fstest.MapFS{
		"0001_first.up.sql":  &fstest.MapFile{Data: []byte("SELECT 1;")},
		"0001_second.up.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
	}
	if _, err := New(pool, dup, testLogger(), "1.0.0").Load(); err == nil {
		t.Fatal("duplicate migration versions should be rejected")
	}
}

func TestLoad_RejectsBadFilename(t *testing.T) {
	pool := newTestDB(t)
	bad := fstest.MapFS{
		"not-a-migration.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	if _, err := New(pool, bad, testLogger(), "1.0.0").Load(); err == nil {
		t.Fatal("a misnamed file should be rejected, not silently skipped")
	}
}

// A fresh install has nothing to backfill, so data migrations are recorded as
// skipped. An existing database queues them for background execution.
func TestDataRunner_FreshInstallSkipsBackfills(t *testing.T) {
	ctx := context.Background()
	log := testLogger()

	t.Run("fresh database marks them skipped", func(t *testing.T) {
		pool := newTestDB(t)
		if err := New(pool, twoMigrations(), log, "1.0.0").Up(ctx, 30*time.Second); err != nil {
			t.Fatal(err)
		}
		r := NewDataRunner(pool, log, "1.0.0", &noopDataMigration{})
		if err := r.Register(ctx, true); err != nil {
			t.Fatalf("register: %v", err)
		}
		assertDataState(t, pool, 100, DataSkipped)
	})

	t.Run("existing database queues them", func(t *testing.T) {
		pool := newTestDB(t)
		if err := New(pool, twoMigrations(), log, "1.0.0").Up(ctx, 30*time.Second); err != nil {
			t.Fatal(err)
		}
		r := NewDataRunner(pool, log, "1.0.0", &noopDataMigration{})
		if err := r.Register(ctx, false); err != nil {
			t.Fatalf("register: %v", err)
		}
		assertDataState(t, pool, 100, DataPending)
	})
}

// A registered backfill must run to completion and be resumable. This also
// proves the cursor is persisted between batches.
func TestDataRunner_RunsToCompletion(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	log := testLogger()

	if err := New(pool, twoMigrations(), log, "1.0.0").Up(ctx, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	dm := &countingDataMigration{batchesUntilDone: 3}
	r := NewDataRunner(pool, log, "1.0.0", dm)
	r.BatchPause = 0

	if err := r.Register(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := r.RunPending(ctx); err != nil {
		t.Fatalf("run pending: %v", err)
	}

	if dm.calls != 3 {
		t.Errorf("Batch called %d times, want 3", dm.calls)
	}
	assertDataState(t, pool, 100, DataCompleted)

	var rowsDone int64
	if err := pool.QueryRow(ctx,
		`SELECT rows_done FROM data_migrations WHERE version = 100`).Scan(&rowsDone); err != nil {
		t.Fatal(err)
	}
	if rowsDone != 30 {
		t.Errorf("rows_done = %d, want 30 (3 batches x 10 rows)", rowsDone)
	}

	// A second run must not re-execute a completed backfill.
	dm.calls = 0
	if err := r.RunPending(ctx); err != nil {
		t.Fatal(err)
	}
	if dm.calls != 0 {
		t.Errorf("completed data migration ran again (%d calls)", dm.calls)
	}
}

// A backfill must not start before the schema it depends on exists.
func TestDataRunner_WaitsForSchemaDependency(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	log := testLogger()

	if err := New(pool, twoMigrations(), log, "1.0.0").Up(ctx, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	dm := &countingDataMigration{batchesUntilDone: 1, requiresSchema: 99}
	r := NewDataRunner(pool, log, "1.0.0", dm)
	r.BatchPause = 0

	if err := r.Register(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := r.RunPending(ctx); err != nil {
		t.Fatalf("run pending: %v", err)
	}
	if dm.calls != 0 {
		t.Errorf("backfill ran despite its schema dependency being unmet (%d calls)", dm.calls)
	}
	assertDataState(t, pool, 100, DataPending)
}

// --- helpers ---

func assertTablesExist(t *testing.T, pool *pgxpool.Pool, names ...string) {
	t.Helper()
	for _, n := range names {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM information_schema.tables
			                WHERE table_schema='public' AND table_name=$1)`, n).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", n, err)
		}
		if !exists {
			t.Errorf("table %q was not created", n)
		}
	}
}

func assertDataState(t *testing.T, pool *pgxpool.Pool, version int64, want DataMigrationState) {
	t.Helper()
	var got string
	err := pool.QueryRow(context.Background(),
		`SELECT state FROM data_migrations WHERE version = $1`, version).Scan(&got)
	if err != nil {
		t.Fatalf("read data_migrations state: %v", err)
	}
	if DataMigrationState(got) != want {
		t.Errorf("data migration %d state = %q, want %q", version, got, want)
	}
}

type noopDataMigration struct{}

func (*noopDataMigration) Version() int64               { return 100 }
func (*noopDataMigration) Name() string                 { return "noop" }
func (*noopDataMigration) RequiresSchemaVersion() int64 { return 1 }
func (*noopDataMigration) EstimateTotal(context.Context, *pgxpool.Pool) (int64, error) {
	return 0, nil
}
func (*noopDataMigration) Batch(context.Context, *pgxpool.Pool, []byte) ([]byte, int64, bool, error) {
	return nil, 0, true, nil
}

type countingDataMigration struct {
	batchesUntilDone int
	requiresSchema   int64
	calls            int
}

func (m *countingDataMigration) Version() int64 { return 100 }
func (m *countingDataMigration) Name() string   { return "counting" }
func (m *countingDataMigration) RequiresSchemaVersion() int64 {
	if m.requiresSchema == 0 {
		return 1
	}
	return m.requiresSchema
}
func (m *countingDataMigration) EstimateTotal(context.Context, *pgxpool.Pool) (int64, error) {
	return int64(m.batchesUntilDone * 10), nil
}
func (m *countingDataMigration) Batch(_ context.Context, _ *pgxpool.Pool, cursor []byte) ([]byte, int64, bool, error) {
	m.calls++
	done := m.calls >= m.batchesUntilDone
	return []byte{byte(m.calls)}, 10, done, nil
}
