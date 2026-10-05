package jobs

import (
	"context"

	"github.com/riverqueue/river"

	"github.com/ergodicregulus/jobtrack/internal/store"
)

// DedupeCompanyWorker collapses one role carried by two of a company's sources.
//
// That is the only duplicate this system can see: a company's ATS board and its
// careers page describing the same role, or a migration between vendors briefly
// doubling everything. Inside one source there is nothing to collapse — the
// vendor's ids are distinct postings by definition — and the version that
// looked there hid 4,310 roles still listed on their boards
// (store.DedupeAcrossSources has the measurement).
//
// A wrong merge HIDES a real job from a user, which is far worse than showing
// one near-duplicate, so every threshold here errs towards under-merging.
type DedupeCompanyWorker struct {
	river.WorkerDefaults[DedupeCompanyArgs]
	Deps *Deps
}

func (w *DedupeCompanyWorker) Work(ctx context.Context, job *river.Job[DedupeCompanyArgs]) error {
	n, err := store.DedupeAcrossSources(ctx, w.Deps.Pool, job.Args.CompanyID)
	if err != nil {
		return err
	}
	// Only log when something happened. "deduped 0" for every company on every
	// change is exactly the clutter that hides real signals.
	if n > 0 {
		w.Deps.Log.InfoContext(ctx, "postings deduplicated",
			"company_id", job.Args.CompanyID, "superseded", n)
	}
	return nil
}
