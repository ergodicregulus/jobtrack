package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/ergodicregulus/jobtrack/internal/config"
	"github.com/ergodicregulus/jobtrack/internal/normalise"
	"github.com/ergodicregulus/jobtrack/internal/source"
	"github.com/ergodicregulus/jobtrack/internal/source/ashby"
	"github.com/ergodicregulus/jobtrack/internal/source/bamboohr"
	"github.com/ergodicregulus/jobtrack/internal/source/greenhouse"
	"github.com/ergodicregulus/jobtrack/internal/source/keka"
	"github.com/ergodicregulus/jobtrack/internal/source/personio"
	"github.com/ergodicregulus/jobtrack/internal/source/recruitee"
	"github.com/ergodicregulus/jobtrack/internal/source/smartrecruiters"
	"github.com/ergodicregulus/jobtrack/internal/source/workable"
	"github.com/ergodicregulus/jobtrack/internal/source/workday"
	"github.com/ergodicregulus/jobtrack/internal/store"
)

// Deps is what every worker needs. Passed once at construction rather than
// through job args, because args are persisted and must stay small and PII-free.
type Deps struct {
	Pool  *pgxpool.Pool
	Log   *slog.Logger
	Cfg   *config.Config
	Vocab *normalise.Vocabulary

	// River is set after the client is built, so workers can enqueue follow-up
	// work inside the same transaction as their own writes.
	River *river.Client[pgx.Tx]

	adapters map[source.Vendor]source.Adapter
	limiter  *hostLimiter
	allow    allowlist
}

// allowlist is INGEST_LIVE_ALLOWLIST, enforced.
//
// It used to be checked only for being non-empty, so INGEST_LIVE_ALLOWLIST=1
// fetched every registered board — the control on outbound traffic to third
// parties guarded a label, the same way INGEST_MODE did before ADR-0021. "*" is an
// explicit opt-in to every registered source, so rebuilding a corpus does not mean
// typing out every ID; the default is still nothing.
type allowlist struct {
	all bool
	ids map[int64]bool
}

// newAllowlist parses the entries config.Load has already validated. Outside live
// mode it permits everything, because a replay makes no request to limit.
func newAllowlist(mode string, entries []string) allowlist {
	if mode != "live" {
		return allowlist{all: true}
	}
	a := allowlist{ids: map[int64]bool{}}
	for _, e := range entries {
		if e == "*" {
			a.all = true
		} else if n, err := strconv.ParseInt(e, 10, 64); err == nil {
			a.ids[n] = true
		}
	}
	return a
}

func (a allowlist) permits(sourceID int64) bool { return a.all || a.ids[sourceID] }

// Init builds the adapter registry and the politeness limiter.
func (d *Deps) Init() {
	client := d.httpClient()
	ua := d.Cfg.Ingest.UserAgent

	d.adapters = map[source.Vendor]source.Adapter{
		source.VendorGreenhouse:      greenhouse.New(client, ua),
		source.VendorAshby:           ashby.New(client, ua),
		source.VendorWorkday:         workday.New(client, ua),
		source.VendorSmartRecruiters: smartrecruiters.New(client, ua),
		source.VendorRecruitee:       recruitee.New(client, ua),
		source.VendorWorkable:        workable.New(client, ua),
		source.VendorPersonio:        personio.New(client, ua),
		source.VendorKeka:            keka.New(client, ua),
		source.VendorBambooHR:        bamboohr.New(client, ua),
	}
	d.limiter = newHostLimiter(2 * time.Second)
	d.allow = newAllowlist(d.Cfg.Ingest.Mode, d.Cfg.Ingest.LiveAllowlist)
}

// httpClient is a real client only in live mode.
//
// INGEST_MODE defaults to fixture, so the DEFAULT is offline: a fresh clone and
// CI both ingest from the golden files and reach nothing. Before ADR-0021 no code
// read the setting at all and every mode fetched live, which made
// INGEST_LIVE_ALLOWLIST a guard on a label rather than on a request.
//
// `recorded` replays as well. It promises "a captured session with realistic
// timing", the timing does not exist, and replaying without it is closer to that
// promise than fetching live would be.
func (d *Deps) httpClient() source.HTTPDoer {
	if d.Cfg.Ingest.Mode != "live" {
		d.Log.Info("ingest is replaying fixtures, no requests will leave this process",
			"mode", d.Cfg.Ingest.Mode)
		return source.NewReplay("")
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

func (d *Deps) adapterFor(v source.Vendor) (source.Adapter, bool) {
	a, ok := d.adapters[v]
	return a, ok
}

// --- schedule_sources -------------------------------------------------------

// ScheduleSourcesWorker enqueues fetches for sources that are due.
type ScheduleSourcesWorker struct {
	river.WorkerDefaults[ScheduleSourcesArgs]
	Deps *Deps
}

func (w *ScheduleSourcesWorker) Work(ctx context.Context, job *river.Job[ScheduleSourcesArgs]) error {
	limit := job.Args.Limit
	if limit <= 0 {
		limit = 200
	}

	// Claim the due sources and push next_poll_at forward in ONE statement.
	// Selecting then updating separately would let a second scheduler tick pick
	// up the same rows before the first had marked them, producing duplicate
	// fetches — the politeness violation this design exists to prevent.
	ids, err := store.ClaimDueSources(ctx, w.Deps.Pool, limit,
		w.Deps.Cfg.Ingest.TierAInterval.String(),
		w.Deps.Cfg.Ingest.TierBInterval.String(),
		w.Deps.Cfg.Ingest.TierCInterval.String())
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	params := make([]river.InsertManyParams, len(ids))
	for i, id := range ids {
		params[i] = river.InsertManyParams{Args: FetchSourceArgs{SourceID: id}}
	}
	if _, err := w.Deps.River.InsertMany(ctx, params); err != nil {
		return fmt.Errorf("enqueue fetches: %w", err)
	}

	w.Deps.Log.InfoContext(ctx, "sources scheduled", "count", len(ids))
	return nil
}

// --- fetch_source -----------------------------------------------------------

// FetchSourceWorker polls one board and upserts what it finds.
type FetchSourceWorker struct {
	river.WorkerDefaults[FetchSourceArgs]
	Deps *Deps
}

func (w *FetchSourceWorker) Work(ctx context.Context, job *river.Job[FetchSourceArgs]) error {
	d := w.Deps

	src, err := store.LoadSource(ctx, d.Pool, job.Args.SourceID)
	if errors.Is(err, store.ErrNotFound) {
		// The source was deleted between scheduling and running. Not an error.
		return nil
	}
	if err != nil {
		return err
	}
	// Checked here, next to the request, rather than when scheduling: this is the
	// one point every fetch passes, so nothing can route around it.
	if !d.allow.permits(src.ID) {
		return nil
	}

	// Force means "ignore the validators and re-read the board", not "throw
	// away the bodies we already fetched": the detail cursor is carried either
	// way, because resetting it would restart a large board's sweep from the
	// top every time.
	if job.Args.Force {
		src.ETag, src.LastModified, src.ContentHash = "", "", nil
	}

	adapter, ok := d.adapterFor(src.Vendor)
	if !ok {
		return fmt.Errorf("no adapter for vendor %q", src.Vendor)
	}

	// Politeness: never more than one in-flight request per host, with an
	// adaptive delay. Global concurrency is high; per-host concurrency is 1.
	release, err := d.limiter.acquire(ctx, string(src.Vendor))
	if err != nil {
		return err
	}
	start := time.Now()
	result, fetchErr := adapter.Fetch(ctx, src)
	release(time.Since(start))

	if fetchErr != nil {
		return w.handleFetchError(ctx, src, result, fetchErr)
	}

	if result.NotModified {
		// The common path — roughly 90% of polls. Record that we looked, and
		// do no further work.
		return store.MarkPollUnchanged(ctx, d.Pool, src.ID, result.ETag, result.LastModified)
	}

	upserted, changed, err := w.persist(ctx, src, result)
	if err != nil {
		return err
	}

	d.Log.InfoContext(ctx, "source ingested",
		"source_id", src.ID, "vendor", src.Vendor, "board", src.BoardToken,
		"postings", upserted, "changed", changed,
		"duration_ms", time.Since(start).Milliseconds())

	// Only bother deduplicating when something actually moved.
	if changed > 0 {
		if _, err := d.River.Insert(ctx, DedupeCompanyArgs{CompanyID: src.CompanyID}, nil); err != nil {
			d.Log.WarnContext(ctx, "could not enqueue dedupe", "error", err)
		}
	}
	return nil
}

// persist writes one poll's results in a single transaction.
//
// The posting upserts, the closure reconciliation and the scoring jobs all
// commit together. There is no window where a posting exists without its
// scoring job, which is why this system needs no outbox.
func (w *FetchSourceWorker) persist(ctx context.Context, src source.Source, result source.FetchResult) (upserted, changed int, err error) {
	d := w.Deps

	err = store.InTx(ctx, d.Pool, func(tx pgx.Tx) error {
		seen := make([]string, 0, len(result.Postings))
		var changedIDs []int64

		for _, raw := range result.Postings {
			p := store.PostingFromRaw(raw, src, d.Vocab)

			res, err := store.UpsertPosting(ctx, tx, p)
			if err != nil {
				return err
			}
			if err := store.ReplacePostingSkills(ctx, tx, res.PostingID, p.Skills); err != nil {
				return err
			}
			if err := store.RecordObservation(ctx, tx, res.PostingID, result.ContentHash); err != nil {
				return err
			}

			seen = append(seen, raw.ExternalID)
			upserted++
			if res.Changed {
				changed++
				changedIDs = append(changedIDs, res.PostingID)
			}
		}

		// Absence is only meaningful in a response we fully received. Never
		// call this after a 304 or a partial payload.
		if _, err := store.ReconcileAbsent(ctx, tx, src.ID, seen); err != nil {
			return err
		}

		if err := store.MarkPollSucceeded(ctx, tx, src.ID, result.ETag,
			result.LastModified, result.ContentHash, result.DetailCursor); err != nil {
			return err
		}

		// Nothing is enqueued for the postings that changed. Scores are
		// computed when a feed is read (ADR-0016), so a new posting is
		// rankable the moment it is committed — there is no fan-out to
		// schedule, and no window during which a posting exists but is
		// unscored.
		return nil
	})
	return upserted, changed, err
}

// handleFetchError records the failure and decides whether to back off.
func (w *FetchSourceWorker) handleFetchError(ctx context.Context, src source.Source, result source.FetchResult, fetchErr error) error {
	d := w.Deps

	var disableFor time.Duration
	switch {
	case errors.Is(fetchErr, source.ErrSourceGone):
		// The board is gone. Back off hard rather than retrying every cycle.
		disableFor = 24 * time.Hour
	case errors.Is(fetchErr, source.ErrRateLimited):
		disableFor = result.RetryAfter
		if disableFor <= 0 {
			disableFor = 15 * time.Minute
		}
	}

	// Circuit breaker: five consecutive failures pauses the source for six
	// hours, so one broken board cannot consume a worker slot forever.
	// Logged rather than returned: the fetch error below is the one worth
	// reporting, and losing the breaker update must not mask it.
	if err := store.MarkPollFailed(ctx, d.Pool, src.ID, disableFor.String()); err != nil {
		d.Log.ErrorContext(ctx, "could not record source failure",
			"source_id", src.ID, "error", err)
	}

	// A dead board is a fact, not a transient failure: returning an error would
	// make River retry it four more times for nothing.
	if errors.Is(fetchErr, source.ErrSourceGone) {
		d.Log.WarnContext(ctx, "source is gone; disabled for 24h",
			"source_id", src.ID, "vendor", src.Vendor, "board", src.BoardToken)
		return nil
	}
	return fetchErr
}
