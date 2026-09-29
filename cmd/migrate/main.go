// Command migrate applies schema migrations and manages background data
// migrations.
//
// It runs as a Kubernetes Job before the application rollout begins, and exits
// non-zero to abort the deploy. Subcommands:
//
//	up      apply pending schema migrations, register data migrations  (deploy step)
//	status  print what is applied and what is pending                  (read-only)
//	verify  exit non-zero if the database is behind the binary         (readiness)
//	data    run pending data migrations to completion                  (manual drain)
//
// Fresh installs and upgrades use the same `up` command. Pending is derived
// from the ledger, so a fresh database runs everything and an upgrade from
// 1.0.0 to 1.0.1 runs only what was added in between. There is no separate
// install path to drift out of sync.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/app"
	"github.com/ergodicregulus/jobtrack/internal/datamigrations"
	"github.com/ergodicregulus/jobtrack/internal/migrate"
	"github.com/ergodicregulus/jobtrack/internal/version"
	"github.com/ergodicregulus/jobtrack/migrations"
)

func main() {
	lockTimeout := flag.Duration("lock-timeout", 5*time.Minute,
		"how long to wait for the migration advisory lock")
	flag.Usage = usage
	flag.Parse()

	cmd := flag.Arg(0)
	if cmd == "" {
		usage()
		os.Exit(2)
	}

	ctx := context.Background()
	a, err := app.New(ctx, app.Options{Service: "migrate", NeedsDB: true})
	if err != nil {
		app.Fatal(err)
	}
	defer a.Close(ctx)

	m := migrate.New(a.Pool, migrations.FS, a.Log, version.Get().Version)
	runner := migrate.NewDataRunner(a.Pool, a.Log, version.Get().Version, datamigrations.All()...)

	if err := run(ctx, cmd, m, runner, *lockTimeout); err != nil {
		a.Log.Error("migrate failed", "command", cmd, "error", err)
		a.Close(ctx)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd string, m *migrate.Migrator, runner *migrate.DataRunner, lockTimeout time.Duration) error {
	switch cmd {
	case "up":
		// Capture freshness *before* applying: after `Up` the ledger is
		// populated and the database no longer looks fresh. This is what lets
		// a new install skip backfills that provably have nothing to do.
		plan, err := m.Status(ctx)
		if err != nil {
			return err
		}
		fresh := plan.IsFreshDatabase

		if err := m.Up(ctx, lockTimeout); err != nil {
			return err
		}
		return runner.Register(ctx, fresh)

	case "status":
		return printStatus(ctx, m, runner)

	case "verify":
		return m.Verify(ctx)

	case "data":
		return runner.RunPending(ctx)

	case "repair":
		arg := flag.Arg(1)
		if arg == "" {
			dirty, err := m.Dirty(ctx)
			if err != nil {
				return err
			}
			if len(dirty) == 0 {
				fmt.Println("no dirty migrations")
				return nil
			}
			for _, d := range dirty {
				fmt.Printf("DIRTY  %04d  %s  (attempted %s by %s)\n",
					d.Version, d.Name, d.AppliedAt.Format(time.RFC3339), d.AppVersion)
			}
			return fmt.Errorf("inspect the database, then run: migrate repair <version>")
		}
		version, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return fmt.Errorf("repair: %q is not a version number", arg)
		}
		return m.Repair(ctx, version)

	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printStatus(ctx context.Context, m *migrate.Migrator, runner *migrate.DataRunner) error {
	plan, err := m.Status(ctx)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "\nSCHEMA\tapplied=%d\tpending=%d\tfresh=%t\n",
		plan.Applied, len(plan.Pending), plan.IsFreshDatabase)
	for _, p := range plan.Pending {
		fmt.Fprintf(w, "  PENDING\t%04d\t%s\n", p.Version, p.Name)
	}
	if len(plan.Pending) == 0 {
		fmt.Fprintf(w, "  up to date\t\t\n")
	}

	data, err := runner.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nDATA MIGRATIONS\t\t\n")
	if len(data) == 0 {
		fmt.Fprintf(w, "  none registered\t\t\n")
	}
	for _, d := range data {
		progress := fmt.Sprintf("%d", d.RowsDone)
		if d.RowsTotal != nil && *d.RowsTotal > 0 {
			progress = fmt.Sprintf("%d/~%d", d.RowsDone, *d.RowsTotal)
		}
		line := fmt.Sprintf("  %-9s\t%04d\t%s\t%s", d.State, d.Version, d.Name, progress)
		if d.LastError != nil {
			line += "\terror: " + *d.LastError
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
	return w.Flush()
}

func usage() {
	fmt.Fprintf(os.Stderr, `jobtrack migrate %s

Usage: migrate [flags] <command>

Commands:
  up      Apply pending schema migrations and register data migrations.
          Safe to run concurrently: an advisory lock serialises instances.
          Same command for a fresh install and an upgrade.
  status  Print applied/pending schema migrations and data-migration progress.
  verify  Exit non-zero if the database is behind this binary. Use as a
          readiness gate so an instance never serves against a stale schema.
  data    Run pending data migrations to completion in the foreground.
          Normally the scheduler does this in the background; use for a
          manual drain before a contract migration.
  repair  List migrations left dirty by a failed run. With a version
          argument, clear that row so the next 'up' retries it.
          Inspect the database first — a failed CREATE INDEX CONCURRENTLY
          leaves an INVALID index that must be dropped, or the retry will
          skip it because of IF NOT EXISTS. See runbook R11.

Flags:
`, version.Get().String())
	flag.PrintDefaults()
}
