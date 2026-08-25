// Package store owns database access.
//
// The pool is configured per service from config, because the per-component
// connection budget is a hard contract: a matcher scale-out event must never
// starve the API of connections. See docs/architecture/service-topology.md §5.
package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/config"
)

// Open creates and verifies a connection pool.
func Open(ctx context.Context, cfg config.Database, log *slog.Logger) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	pc.MaxConns = cfg.MaxConns
	pc.MinConns = cfg.MinConns
	pc.MaxConnLifetime = cfg.MaxConnLifetime
	pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	// Jitter prevents every connection in the pool reaching MaxConnLifetime at
	// the same instant and reconnecting in a thundering herd.
	pc.MaxConnLifetimeJitter = cfg.MaxConnLifetime / 10

	// A statement timeout is the last line of defence against a runaway query
	// holding a connection forever. Set at the session level so it applies to
	// everything, including queries we did not write.
	if cfg.StatementTimeout > 0 {
		pc.ConnConfig.RuntimeParams["statement_timeout"] =
			fmt.Sprintf("%d", cfg.StatementTimeout.Milliseconds())
	}
	// Kill sessions that hold a transaction open without doing work. This is
	// the single most common cause of connection exhaustion (runbook R5).
	pc.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60000"

	// Application name shows up in pg_stat_activity, which makes "which service
	// is eating the pool?" answerable in one query instead of by guesswork.
	pc.ConnConfig.RuntimeParams["application_name"] = "jobtrack"

	// JIT off.
	//
	// Not a superstition: the feed's candidate query measured 1,009 ms, of
	// which 517 ms was JIT — 119 ms inlining, 257 ms optimising, 136 ms
	// emitting — to run a plan that takes 300 ms without it. Postgres decides
	// to JIT on estimated cost, and a query that touches ten thousand rows
	// clears the default threshold while gaining nothing, because the work is
	// I/O and not expression evaluation.
	//
	// This is an OLTP service where every budget is in the tens of milliseconds
	// and no query is analytical. If one ever is, turn it on for that statement
	// rather than for the pool.
	pc.ConnConfig.RuntimeParams["jit"] = "off"

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	log.Info("database connected",
		"max_conns", cfg.MaxConns,
		"min_conns", cfg.MinConns,
		"statement_timeout", cfg.StatementTimeout.String())

	return pool, nil
}

// InTx runs fn inside a transaction, rolling back on error or panic.
//
// Every multi-statement write goes through this. The alternative — hand-rolled
// Begin/Commit at each call site — reliably produces one path that forgets to
// roll back, and that path becomes an idle-in-transaction connection leak.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			// WithoutCancel: if the request context was cancelled we still need
			// to roll back, or the connection is returned mid-transaction.
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
