package migrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DataMigration is a long-running backfill that runs *after* the application is
// serving traffic.
//
// The distinction from a schema migration is not size, it is blocking: a schema
// migration holds up the deploy, so it must be fast. A backfill over 200 million
// rows cannot be fast, so it must not block. Running one as a schema migration
// is the single most common way to turn a deploy into an outage.
//
// Implementations must be:
//   - Batched — never one statement over the whole table.
//   - Resumable — Batch receives the cursor it last returned.
//   - Idempotent — a batch may run twice after a crash.
type DataMigration interface {
	// Version orders execution and keys the ledger. Shares the number space
	// with schema migrations so ordering between the two is unambiguous.
	Version() int64

	// Name is the human label; must match the ledger for the life of the row.
	Name() string

	// RequiresSchemaVersion is the schema migration this backfill depends on.
	// The runner refuses to start until that version is applied — a backfill
	// writing to a column that does not exist yet fails in a confusing way.
	RequiresSchemaVersion() int64

	// Batch processes one chunk starting from cursor and returns the next
	// cursor, how many rows it touched, and whether work remains.
	// A nil cursor means "start from the beginning".
	Batch(ctx context.Context, pool *pgxpool.Pool, cursor []byte) (next []byte, rows int64, done bool, err error)

	// EstimateTotal is used for progress reporting only. Return 0 if unknown —
	// a wrong estimate is worse than none.
	EstimateTotal(ctx context.Context, pool *pgxpool.Pool) (int64, error)
}

// DataMigrationState mirrors the SQL enum.
type DataMigrationState string

const (
	DataPending   DataMigrationState = "pending"
	DataRunning   DataMigrationState = "running"
	DataCompleted DataMigrationState = "completed"
	DataFailed    DataMigrationState = "failed"
	DataSkipped   DataMigrationState = "skipped"
)

// DataRunner executes registered data migrations.
type DataRunner struct {
	pool       *pgxpool.Pool
	log        *slog.Logger
	appVersion string
	registry   []DataMigration

	// BatchPause throttles between batches so a backfill cannot monopolise the
	// database. Deliberately a field rather than a constant: an operator
	// draining a backlog overnight wants it lower.
	BatchPause time.Duration
}

func NewDataRunner(pool *pgxpool.Pool, log *slog.Logger, appVersion string, migrations ...DataMigration) *DataRunner {
	sorted := append([]DataMigration(nil), migrations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version() < sorted[j].Version() })
	return &DataRunner{
		pool:       pool,
		log:        log,
		appVersion: appVersion,
		registry:   sorted,
		BatchPause: 100 * time.Millisecond,
	}
}

// EnsureLedger creates the data-migration ledger.
func (r *DataRunner) EnsureLedger(ctx context.Context) error {
	const ddl = `
DO $$ BEGIN
    CREATE TYPE data_migration_state AS ENUM ('pending','running','completed','failed','skipped');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS data_migrations (
    version      bigint PRIMARY KEY,
    name         text                 NOT NULL,
    state        data_migration_state NOT NULL DEFAULT 'pending',
    cursor       bytea,
    rows_done    bigint               NOT NULL DEFAULT 0,
    rows_total   bigint,
    last_error   text,
    attempts     int                  NOT NULL DEFAULT 0,
    app_version  text                 NOT NULL,
    started_at   timestamptz,
    completed_at timestamptz,
    updated_at   timestamptz          NOT NULL DEFAULT now()
);`
	_, err := r.pool.Exec(ctx, ddl)
	return err
}

// Register inserts ledger rows for any data migration not yet known.
//
// isFreshDatabase is the important argument. On a fresh install the tables a
// backfill would scan are empty, so the work is provably a no-op. Marking those
// rows 'skipped' immediately keeps the ledger honest about *why* they never ran,
// rather than leaving a permanently-pending row that looks like a stuck backfill.
//
// Mis-detecting freshness is harmless in both directions, which is why this is a
// simple boolean rather than a per-table emptiness check: a backfill wrongly
// marked pending finds no rows in its first batch and completes immediately.
// The flag is an honesty improvement, not a correctness requirement.
func (r *DataRunner) Register(ctx context.Context, isFreshDatabase bool) error {
	if err := r.EnsureLedger(ctx); err != nil {
		return err
	}

	state := DataPending
	if isFreshDatabase {
		state = DataSkipped
	}

	for _, dm := range r.registry {
		ct, err := r.pool.Exec(ctx,
			`INSERT INTO data_migrations (version, name, state, app_version, completed_at)
			 VALUES ($1, $2, $3, $4, CASE WHEN $3 = 'skipped'::data_migration_state THEN now() END)
			 ON CONFLICT (version) DO NOTHING`,
			dm.Version(), dm.Name(), string(state), r.appVersion)
		if err != nil {
			return fmt.Errorf("register data migration %d: %w", dm.Version(), err)
		}
		if ct.RowsAffected() > 0 {
			r.log.Info("registered data migration",
				"version", dm.Version(), "name", dm.Name(), "state", state,
				"reason", freshReason(isFreshDatabase))
		}
	}
	return nil
}

func freshReason(fresh bool) string {
	if fresh {
		return "fresh database — no rows to backfill"
	}
	return "queued for background execution"
}

// RunPending executes every data migration that is pending or previously failed.
// Called on a schedule by the scheduler service, so a crashed backfill resumes
// on the next tick without operator action.
func (r *DataRunner) RunPending(ctx context.Context) error {
	if err := r.EnsureLedger(ctx); err != nil {
		return err
	}

	for _, dm := range r.registry {
		state, cursor, err := r.load(ctx, dm.Version())
		if err != nil {
			return err
		}
		if state == DataCompleted || state == DataSkipped {
			continue
		}
		if err := r.runOne(ctx, dm, cursor); err != nil {
			// One failing backfill must not stop the others — they are
			// independent, and stopping would hide the rest behind one bug.
			r.log.Error("data migration failed",
				"version", dm.Version(), "name", dm.Name(), "error", err)
		}
	}
	return nil
}

func (r *DataRunner) load(ctx context.Context, version int64) (DataMigrationState, []byte, error) {
	var state string
	var cursor []byte
	err := r.pool.QueryRow(ctx,
		`SELECT state, cursor FROM data_migrations WHERE version = $1`, version).
		Scan(&state, &cursor)
	if err != nil {
		return "", nil, fmt.Errorf("load data migration %d: %w", version, err)
	}
	return DataMigrationState(state), cursor, nil
}

func (r *DataRunner) runOne(ctx context.Context, dm DataMigration, cursor []byte) error {
	log := r.log.With("version", dm.Version(), "name", dm.Name())

	claimed, err := r.claim(ctx, dm, log)
	if err != nil || !claimed {
		return err
	}

	if total, err := dm.EstimateTotal(ctx, r.pool); err == nil && total > 0 {
		_, _ = r.pool.Exec(ctx, `UPDATE data_migrations SET rows_total = $2 WHERE version = $1`,
			dm.Version(), total)
	}

	log.Info("data migration started")
	start := time.Now()

	processed, err := r.runBatches(ctx, dm, cursor, log)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		// Paused, not finished. runBatches has already reset the state.
		return nil
	}

	if _, err := r.pool.Exec(ctx,
		`UPDATE data_migrations
		    SET state='completed', completed_at=now(), updated_at=now(), last_error=NULL
		  WHERE version=$1`, dm.Version()); err != nil {
		return fmt.Errorf("mark complete: %w", err)
	}

	log.Info("data migration complete",
		"rows", processed, "duration_s", int64(time.Since(start).Seconds()))
	return nil
}

// claim takes ownership of a migration, or reports that it cannot.
//
// The WHERE clause IS the lock: two schedulers racing means one UPDATE affects
// zero rows and that instance backs off. No advisory lock, no coordination —
// the row is the mutex.
//
// It also refuses to run ahead of the schema it depends on, which is why this
// returns (false, nil) rather than an error for both cases: neither "someone
// else has it" nor "the schema is not ready yet" is a failure, and treating them
// as one would fill the log with alarms on every tick of a healthy system.
func (r *DataRunner) claim(ctx context.Context, dm DataMigration, log *slog.Logger) (bool, error) {
	var schemaOK bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version >= $1 AND NOT dirty)`,
		dm.RequiresSchemaVersion()).Scan(&schemaOK); err != nil {
		return false, fmt.Errorf("check schema dependency: %w", err)
	}
	if !schemaOK {
		log.Warn("waiting for schema migration", "requires", dm.RequiresSchemaVersion())
		return false, nil
	}

	ct, err := r.pool.Exec(ctx,
		`UPDATE data_migrations
		    SET state = 'running', attempts = attempts + 1, app_version = $2,
		        started_at = COALESCE(started_at, now()), updated_at = now()
		  WHERE version = $1 AND state IN ('pending','failed')`,
		dm.Version(), r.appVersion)
	if err != nil {
		return false, fmt.Errorf("claim: %w", err)
	}
	if ct.RowsAffected() == 0 {
		log.Debug("already claimed by another instance")
		return false, nil
	}
	return true, nil
}

// runBatches walks the migration to completion, persisting the cursor after
// every batch.
//
// Shutdown mid-backfill is normal and safe precisely because of that: the state
// goes back to 'pending' and the next tick resumes from the stored cursor. The
// WithoutCancel is load-bearing — writing the pause with the cancelled context
// would fail, and the migration would look 'running' forever with no process
// running it.
func (r *DataRunner) runBatches(
	ctx context.Context, dm DataMigration, cursor []byte, log *slog.Logger,
) (int64, error) {
	var processed int64

	for {
		if err := ctx.Err(); err != nil {
			_, _ = r.pool.Exec(context.WithoutCancel(ctx),
				`UPDATE data_migrations SET state='pending', updated_at=now() WHERE version=$1`,
				dm.Version())
			log.Info("data migration paused for shutdown", "rows_done", processed)
			return processed, nil
		}

		next, rows, done, err := dm.Batch(ctx, r.pool, cursor)
		if err != nil {
			_, _ = r.pool.Exec(context.WithoutCancel(ctx),
				`UPDATE data_migrations SET state='failed', last_error=$2, updated_at=now() WHERE version=$1`,
				dm.Version(), err.Error())
			return processed, fmt.Errorf("batch: %w", err)
		}

		processed += rows
		cursor = next

		if _, err := r.pool.Exec(ctx,
			`UPDATE data_migrations SET cursor=$2, rows_done=rows_done+$3, updated_at=now() WHERE version=$1`,
			dm.Version(), cursor, rows); err != nil {
			return processed, fmt.Errorf("persist cursor: %w", err)
		}

		if done {
			return processed, nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(r.BatchPause):
		}
	}
}

// DataStatus is one row for `migrate status`.
type DataStatus struct {
	Version   int64
	Name      string
	State     DataMigrationState
	RowsDone  int64
	RowsTotal *int64
	LastError *string
}

func (r *DataRunner) Status(ctx context.Context) ([]DataStatus, error) {
	if err := r.EnsureLedger(ctx); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT version, name, state, rows_done, rows_total, last_error
		   FROM data_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DataStatus
	for rows.Next() {
		var s DataStatus
		var state string
		if err := rows.Scan(&s.Version, &s.Name, &state, &s.RowsDone, &s.RowsTotal, &s.LastError); err != nil {
			return nil, err
		}
		s.State = DataMigrationState(state)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ErrNotRegistered is returned when the ledger references an unknown migration,
// which means a rollback removed code the database still expects.
var ErrNotRegistered = errors.New("migrate: data migration in ledger is not registered in this binary")
