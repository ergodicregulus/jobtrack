package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/config"
	"github.com/ergodicregulus/jobtrack/internal/telemetry"
)

// The last of the three packages CLAUDE.md's gap table named. What is worth
// testing here is shutdown ORDER, because the comment on Run calls it "the whole
// point" and nothing verified it: a process that tears down its pool before its
// server has drained turns every in-flight request into an error, and that
// failure is invisible until it happens in production under load.

func testApp(t *testing.T) *App {
	t.Helper()
	return &App{
		Cfg: &config.Config{ShutdownTimeout: 2 * time.Second},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Zero value: Shutdown returns nil when no tracer was installed.
		tel:      &telemetry.Provider{},
		startCtx: context.Background(),
	}
}

func TestRun_ReturnsWhatFnReturned(t *testing.T) {
	a := testApp(t)
	want := errors.New("the service failed")

	if got := a.Run(func(context.Context) error { return want }); !errors.Is(got, want) {
		t.Errorf("Run returned %v, want %v", got, want)
	}
}

// A cancelled context is how a drained service reports that it stopped because it
// was ASKED to. Surfacing it as a failure would make every clean shutdown exit
// non-zero, which turns an orderly deploy into a red alert.
func TestRun_TreatsContextCanceledAsACleanStop(t *testing.T) {
	a := testApp(t)
	if err := a.Run(func(context.Context) error { return context.Canceled }); err != nil {
		t.Errorf("Run returned %v for a deliberate cancellation, want nil", err)
	}
}

// Reverse order, always. Closers are appended as dependencies are acquired, so
// releasing them in reverse is the only order in which nothing is torn down while
// something that depends on it is still open.
func TestClose_RunsClosersInReverseOrder(t *testing.T) {
	a := testApp(t)
	var order []string
	for _, name := range []string{"first", "second", "third"} {
		a.closers = append(a.closers, func(context.Context) error {
			order = append(order, name)
			return nil
		})
	}

	a.Close(context.Background())

	want := []string{"third", "second", "first"}
	if len(order) != len(want) {
		t.Fatalf("ran %d closers, want %d: %v", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("closers ran %v, want %v — acquisition order reversed", order, want)
		}
	}
}

// One closer failing must not abandon the rest. A pool that cannot be closed is
// not a reason to leak every file handle acquired before it, and the error is
// logged rather than returned because there is nobody left to hand it to.
func TestClose_ContinuesAfterAFailingCloser(t *testing.T) {
	a := testApp(t)
	reached := false
	a.closers = append(a.closers,
		func(context.Context) error { reached = true; return nil },
		func(context.Context) error { return errors.New("could not close") },
	)

	a.Close(context.Background())

	if !reached {
		t.Error("a failing closer stopped the ones acquired before it; those resources leak")
	}
}

// Run must drain before it tears down. If Close ran first, fn would still be
// using connections that had already gone away.
func TestRun_ClosesOnlyAfterFnHasReturned(t *testing.T) {
	a := testApp(t)
	var events []string
	a.closers = append(a.closers, func(context.Context) error {
		events = append(events, "closed")
		return nil
	})

	err := a.Run(func(context.Context) error {
		events = append(events, "fn returned")
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(events) != 2 || events[0] != "fn returned" || events[1] != "closed" {
		t.Errorf("order was %v, want [fn returned closed] — teardown must follow the drain", events)
	}
}

// freeAddr returns a loopback address nothing is listening on.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// A worker answers probes while it runs. /readyz is 503 without a database: it
// reports what River actually needs, not merely that the process exists.
func TestRunWorker_ServesProbesWhileTheWorkerRuns(t *testing.T) {
	a := testApp(t)
	a.Cfg.HTTPAddr = freeAddr(t)

	codes := make(chan [2]int, 1)
	err := a.RunWorker(func(ctx context.Context) error {
		var got [2]int
		for i, path := range []string{"/livez", "/readyz"} {
			var resp *http.Response
			var err error
			for try := 0; try < 50; try++ { // the server starts concurrently
				if resp, err = http.Get("http://" + a.Cfg.HTTPAddr + path); err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err != nil {
				return err
			}
			resp.Body.Close()
			got[i] = resp.StatusCode
		}
		codes <- got
		return nil
	})
	if err != nil {
		t.Fatalf("RunWorker: %v", err)
	}
	got := <-codes
	if got[0] != http.StatusNoContent {
		t.Errorf("/livez = %d, want 204", got[0])
	}
	if got[1] != http.StatusServiceUnavailable {
		t.Errorf("/readyz without a database = %d, want 503", got[1])
	}
}

// If the health server cannot bind, the worker is stopped and the error
// surfaces. A worker nobody can probe is the failure this exists to end.
func TestRunWorker_StopsTheWorkerWhenItCannotBeProbed(t *testing.T) {
	a := testApp(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	a.Cfg.HTTPAddr = taken.Addr().String()

	stopped := make(chan struct{})
	runErr := a.RunWorker(func(ctx context.Context) error {
		<-ctx.Done() // a real worker runs until told to stop
		close(stopped)
		return ctx.Err()
	})

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker kept running although its health server never bound")
	}
	if runErr == nil {
		t.Error("RunWorker returned nil; the bind failure must surface")
	}
}
