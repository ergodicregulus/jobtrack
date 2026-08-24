package jobs

import (
	"context"

	"github.com/riverqueue/river"

	"github.com/jobtrack/jobtrack/internal/store"
)

// DedupeCompanyWorker collapses duplicate postings within one company.
//
// One requisition legitimately appears more than once: a company's Greenhouse
// board and its JSON-LD careers page describe the same role, and a migration
// between ATS vendors briefly doubles everything. Showing it twice degrades the
// feed; showing it three times destroys trust in it.
//
// Two stages, cheapest first — the shape the Fraunhofer duplicate-detection work
// validated, whose central finding is that no single technique performs
// acceptably alone:
//
//  1. Exact:  same company + same requisition_id. Free, and certain.
//  2. Fuzzy:  trigram blocking on the normalised title, then a conservative
//     similarity floor plus location and work-mode agreement.
//
// The embedding stage described in the design is deliberately NOT here yet. A
// stage that does not exist must not silently pass everything through, and
// stages 1-2 resolve the large majority.
type DedupeCompanyWorker struct {
	river.WorkerDefaults[DedupeCompanyArgs]
	Deps *Deps
}

func (w *DedupeCompanyWorker) Work(ctx context.Context, job *river.Job[DedupeCompanyArgs]) error {
	exact, err := w.mergeByRequisition(ctx, job.Args.CompanyID)
	if err != nil {
		return err
	}
	fuzzy, err := w.mergeBySimilarity(ctx, job.Args.CompanyID)
	if err != nil {
		return err
	}

	// Only log when something happened. "deduped 0" every fifteen minutes for
	// every company is exactly the clutter that hides real signals.
	if exact+fuzzy > 0 {
		w.Deps.Log.InfoContext(ctx, "postings deduplicated",
			"company_id", job.Args.CompanyID,
			"by_requisition", exact, "by_similarity", fuzzy)
	}
	return nil
}

// mergeByRequisition is the free case.
//
// Greenhouse exposes requisition_id. Two live postings from one company sharing
// it ARE the same role — no similarity computation, no threshold, no judgement
// call that could be wrong.
func (w *DedupeCompanyWorker) mergeByRequisition(ctx context.Context, companyID int64) (int64, error) {
	return store.DedupeByRequisition(ctx, w.Deps.Pool, companyID)
}

// mergeBySimilarity handles postings with no shared requisition id.
//
// Blocking comes first: only pairs with high title-trigram similarity are ever
// compared, which is what keeps this from being O(n²) over a 500-role board.
//
// The 0.75 floor is deliberately conservative. A wrong merge HIDES a real job
// from a user, which is far worse than showing one near-duplicate — so the
// threshold is set to under-merge rather than over-merge.
func (w *DedupeCompanyWorker) mergeBySimilarity(ctx context.Context, companyID int64) (int64, error) {
	return store.DedupeBySimilarity(ctx, w.Deps.Pool, companyID)
}
