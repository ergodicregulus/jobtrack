// Package migrate applies schema and data migrations.
//
// Two kinds exist and they solve different problems:
//
//   - Schema migrations are versioned SQL, applied to completion before any
//     application instance starts. They must be fast and backward-compatible
//     with the previous release (expand/contract).
//   - Data migrations run in the background after the application is serving.
//     They are batched, resumable, and may take hours. See data.go.
//
// Both use the same ledger idea: a table records what has been applied, and
// "pending" is derived as (all - applied). That single mechanism is what makes
// a fresh install and an upgrade the same code path — a fresh database has an
// empty ledger so everything is pending, and an upgrade from 1.0.0 to 1.0.1
// finds only the migrations added since.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockKey guards the whole migration process. Two migrators starting
// simultaneously — which happens on every parallel deploy — must not both try
// to apply the same migration.
const advisoryLockKey int64 = 8_675_309_001

var (
	ErrChecksumMismatch = errors.New("migrate: applied migration has been modified")
	ErrDirty            = errors.New("migrate: previous migration failed and left the database dirty")
	ErrLockTimeout      = errors.New("migrate: timed out waiting for the migration lock")
)

// filenamePattern matches `0007_add_yoe_confidence.up.sql`.
var filenamePattern = regexp.MustCompile(`^(\d{4,})_([a-z0-9_]+)\.up\.sql$`)

// directivePattern matches `-- +migrate no-transaction`.
var directivePattern = regexp.MustCompile(`(?m)^--\s*\+migrate\s+([a-z-]+)\s*$`)

// Migration is one versioned schema change.
type Migration struct {
	Version int64
	Name    string
	SQL     string

	// NoTransaction is set by the `-- +migrate no-transaction` directive.
	// Required for CREATE INDEX CONCURRENTLY and ALTER TYPE ... ADD VALUE,
	// which Postgres refuses to run inside a transaction block.
	NoTransaction bool

	// Checksum detects a migration being edited after it was applied. Editing
	// applied migrations produces databases that agree on version numbers and
	// disagree on schema, which is the worst failure mode in this area.
	Checksum string
}

// Applied is one row of the ledger.
type Applied struct {
	Version    int64
	Name       string
	Checksum   string
	AppliedAt  time.Time
	DurationMS int64
	AppVersion string
}

// Migrator applies schema migrations from a filesystem.
type Migrator struct {
	pool       *pgxpool.Pool
	fsys       fs.FS
	log        *slog.Logger
	appVersion string
}

func New(pool *pgxpool.Pool, fsys fs.FS, log *slog.Logger, appVersion string) *Migrator {
	return &Migrator{pool: pool, fsys: fsys, log: log, appVersion: appVersion}
}

// Load reads and parses every migration, sorted by version.
func (m *Migrator) Load() ([]Migration, error) {
	entries, err := fs.ReadDir(m.fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var out []Migration
	seen := make(map[int64]string, len(entries))

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		match := filenamePattern.FindStringSubmatch(e.Name())
		if match == nil {
			// Anything that is not a migration is a mistake, not something to
			// skip silently — a typo'd filename would otherwise never run.
			return nil, fmt.Errorf("migrations: %q does not match NNNN_name.up.sql", e.Name())
		}

		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migrations: bad version in %q: %w", e.Name(), err)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations: version %d used by both %q and %q", version, prev, e.Name())
		}
		seen[version] = e.Name()

		body, err := fs.ReadFile(m.fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}

		sum := sha256.Sum256(body)
		out = append(out, Migration{
			Version:       version,
			Name:          match[2],
			SQL:           string(body),
			NoTransaction: hasDirective(string(body), "no-transaction"),
			Checksum:      hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func hasDirective(sql, want string) bool {
	for _, match := range directivePattern.FindAllStringSubmatch(sql, -1) {
		if match[1] == want {
			return true
		}
	}
	return false
}

// querier is satisfied by both *pgx.Conn and *pgxpool.Pool.
//
// Every internal step takes one of these rather than reaching for m.pool,
// because Up must perform all of its work on the *same* connection that holds
// the advisory lock. Acquiring a second connection while others are blocked
// waiting for that lock deadlocks the pool: the holder cannot proceed, so the
// lock is never released. With the migrate service's pool of 2, a parallel
// deploy would hang until the lock timeout.
type querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ensureLedger creates the ledger table. It is deliberately not itself a
// migration — bootstrapping the thing that tracks migrations with a migration
// is circular.
func (m *Migrator) ensureLedger(ctx context.Context, q querier) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     bigint PRIMARY KEY,
    name        text        NOT NULL,
    checksum    text        NOT NULL,
    applied_at  timestamptz NOT NULL DEFAULT now(),
    duration_ms bigint      NOT NULL,
    app_version text        NOT NULL,
    -- Set while a migration is in flight and cleared on success. A row left
    -- with dirty=true means a previous run died mid-migration.
    dirty       boolean     NOT NULL DEFAULT false
);`
	_, err := q.Exec(ctx, ddl)
	return err
}

// applied returns the ledger keyed by version.
func (m *Migrator) applied(ctx context.Context, q querier) (map[int64]Applied, error) {
	rows, err := q.Query(ctx,
		`SELECT version, name, checksum, applied_at, duration_ms, app_version, dirty
		   FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]Applied)
	for rows.Next() {
		var a Applied
		var dirty bool
		if err := rows.Scan(&a.Version, &a.Name, &a.Checksum, &a.AppliedAt,
			&a.DurationMS, &a.AppVersion, &dirty); err != nil {
			return nil, err
		}
		if dirty {
			return nil, fmt.Errorf("%w: version %d (%s) — inspect the database before retrying",
				ErrDirty, a.Version, a.Name)
		}
		out[a.Version] = a
	}
	return out, rows.Err()
}

// Plan describes what Up would do. Producing it without applying anything is
// what makes `migrate status` and the deploy-time dry run possible.
type Plan struct {
	IsFreshDatabase bool
	Applied         int
	Pending         []Migration
}

func (m *Migrator) plan(ctx context.Context, q querier) (Plan, error) {
	all, err := m.Load()
	if err != nil {
		return Plan{}, err
	}
	done, err := m.applied(ctx, q)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{IsFreshDatabase: len(done) == 0, Applied: len(done)}

	for _, mig := range all {
		prev, ok := done[mig.Version]
		if !ok {
			plan.Pending = append(plan.Pending, mig)
			continue
		}
		if prev.Checksum != mig.Checksum {
			return Plan{}, fmt.Errorf("%w: version %d (%s) applied at %s has checksum %s but the file now hashes to %s",
				ErrChecksumMismatch, mig.Version, mig.Name,
				prev.AppliedAt.Format(time.RFC3339), prev.Checksum[:12], mig.Checksum[:12])
		}
	}
	return plan, nil
}

// Status reports without changing anything.
func (m *Migrator) Status(ctx context.Context) (Plan, error) {
	if err := m.ensureLedger(ctx, m.pool); err != nil {
		return Plan{}, err
	}
	return m.plan(ctx, m.pool)
}

// Up applies every pending migration in version order.
//
// This is the whole fresh-vs-upgrade story: pending is derived from the ledger,
// so a fresh database runs everything and an existing one runs only what was
// added since it was last migrated. There is no separate "install" path to
// drift out of sync with the "upgrade" path.
func (m *Migrator) Up(ctx context.Context, lockTimeout time.Duration) error {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	lockCtx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	if _, err := conn.Exec(lockCtx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return fmt.Errorf("%w after %s: %w", ErrLockTimeout, lockTimeout, err)
	}
	defer func() {
		// Best effort: a released connection drops the lock anyway.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
	}()

	// Everything below runs on `conn` — the connection holding the lock.
	if err := m.ensureLedger(ctx, conn); err != nil {
		return err
	}

	// Re-plan while holding the lock. Another instance may have applied
	// everything between our start and acquiring it, which is the normal case
	// during a parallel deploy.
	plan, err := m.plan(ctx, conn)
	if err != nil {
		return err
	}

	if len(plan.Pending) == 0 {
		m.log.Info("schema up to date", "applied", plan.Applied)
		return nil
	}

	m.log.Info("applying schema migrations",
		"pending", len(plan.Pending),
		"already_applied", plan.Applied,
		"fresh_database", plan.IsFreshDatabase,
		"app_version", m.appVersion)

	for _, mig := range plan.Pending {
		if err := m.apply(ctx, conn.Conn(), mig); err != nil {
			return fmt.Errorf("migration %04d_%s: %w", mig.Version, mig.Name, err)
		}
	}

	m.log.Info("schema migrations complete", "applied", len(plan.Pending))
	return nil
}

func (m *Migrator) apply(ctx context.Context, conn *pgx.Conn, mig Migration) error {
	start := time.Now()
	log := m.log.With("version", mig.Version, "name", mig.Name, "no_transaction", mig.NoTransaction)
	log.Info("applying migration")

	record := func(ctx context.Context, q pgx.Tx) error {
		_, err := q.Exec(ctx,
			`INSERT INTO schema_migrations (version, name, checksum, duration_ms, app_version, dirty)
			 VALUES ($1,$2,$3,$4,$5,false)`,
			mig.Version, mig.Name, mig.Checksum, time.Since(start).Milliseconds(), m.appVersion)
		return err
	}

	if !mig.NoTransaction {
		// The common case: DDL and the ledger row commit together, so a crash
		// can never leave a migration applied but unrecorded.
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, mig.SQL); err != nil {
			return fmt.Errorf("exec: %w", err)
		}
		if err := record(ctx, tx); err != nil {
			return fmt.Errorf("record: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		log.Info("migration applied", "duration_ms", time.Since(start).Milliseconds())
		return nil
	}

	// CREATE INDEX CONCURRENTLY cannot run in a transaction, so the DDL and the
	// ledger row cannot commit atomically. We mark the row dirty first; if the
	// process dies mid-migration the next run refuses to proceed rather than
	// silently skipping or re-running a half-built index.
	if _, err := conn.Exec(ctx,
		`INSERT INTO schema_migrations (version, name, checksum, duration_ms, app_version, dirty)
		 VALUES ($1,$2,$3,0,$4,true)`,
		mig.Version, mig.Name, mig.Checksum, m.appVersion); err != nil {
		return fmt.Errorf("mark dirty: %w", err)
	}

	// One statement per round trip. Postgres wraps a multi-statement query
	// string in an *implicit* transaction, which defeats the entire purpose of
	// this branch — CONCURRENTLY would fail with "cannot run inside a
	// transaction block" even though we never began one.
	statements := splitStatements(mig.SQL)
	for idx, stmt := range statements {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec statement %d/%d (non-transactional, ledger row left dirty for inspection): %w",
				idx+1, len(statements), err)
		}
	}

	if _, err := conn.Exec(ctx,
		`UPDATE schema_migrations SET dirty = false, duration_ms = $2, applied_at = now() WHERE version = $1`,
		mig.Version, time.Since(start).Milliseconds()); err != nil {
		return fmt.Errorf("clear dirty: %w", err)
	}

	log.Info("migration applied", "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// Dirty returns the versions left in a dirty state by a failed run.
func (m *Migrator) Dirty(ctx context.Context) ([]Applied, error) {
	if err := m.ensureLedger(ctx, m.pool); err != nil {
		return nil, err
	}
	rows, err := m.pool.Query(ctx,
		`SELECT version, name, checksum, applied_at, duration_ms, app_version
		   FROM schema_migrations WHERE dirty ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Applied
	for rows.Next() {
		var a Applied
		if err := rows.Scan(&a.Version, &a.Name, &a.Checksum, &a.AppliedAt,
			&a.DurationMS, &a.AppVersion); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Repair clears a dirty ledger row so the migration can be retried.
//
// This is deliberately a separate, explicit operation rather than something Up
// does automatically. A dirty row means a non-transactional migration died
// partway, and the database may hold an INVALID index that a retry would skip
// because of IF NOT EXISTS. An operator must look before the retry — the
// runbook tells them what to look for.
func (m *Migrator) Repair(ctx context.Context, version int64) error {
	// Reindexing an invalid index is not something we can decide for the
	// operator, but leaving one behind silently is worse. Report them.
	invalid, err := m.invalidIndexes(ctx)
	if err != nil {
		return err
	}
	for _, idx := range invalid {
		m.log.Warn("invalid index present; DROP INDEX it before retrying", "index", idx)
	}

	ct, err := m.pool.Exec(ctx,
		`DELETE FROM schema_migrations WHERE version = $1 AND dirty`, version)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("no dirty ledger row for version %d", version)
	}
	m.log.Info("cleared dirty ledger row; migration will be retried on next up", "version", version)
	return nil
}

func (m *Migrator) invalidIndexes(ctx context.Context) ([]string, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT c.relname
		  FROM pg_index i
		  JOIN pg_class c ON c.oid = i.indexrelid
		 WHERE NOT i.indisvalid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// Verify checks that the ledger and the files agree, without applying anything.
// Every service calls this at startup: an instance whose binary expects a
// schema the database does not have should refuse to serve rather than fail
// later on a missing column.
func (m *Migrator) Verify(ctx context.Context) error {
	plan, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if len(plan.Pending) > 0 {
		versions := make([]string, 0, len(plan.Pending))
		for _, p := range plan.Pending {
			versions = append(versions, strconv.FormatInt(p.Version, 10))
		}
		return fmt.Errorf("database is behind the binary: %d pending migration(s): %s",
			len(plan.Pending), strings.Join(versions, ", "))
	}
	return nil
}
