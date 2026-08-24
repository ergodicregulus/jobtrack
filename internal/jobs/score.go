package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/jobtrack/jobtrack/internal/matching"
	"github.com/jobtrack/jobtrack/internal/store"
)

// ScorePostingWorker scores one posting for the users it could plausibly suit.
//
// The fan-out is bounded here, and that boundary is the single most important
// capacity decision in the system. Scoring every posting against every user is
// O(users × postings) and would dominate everything else, so a posting is only
// scored for users whose coarse preferences make it relevant AND who have been
// active recently — nobody is waiting on a score they will never look at.
type ScorePostingWorker struct {
	river.WorkerDefaults[ScorePostingArgs]
	Deps   *Deps
	Scorer *matching.Scorer
}

// activeWindow bounds fan-out to users who might actually see the result.
// Dormant users are scored lazily on their next session instead.
const activeWindow = 30 * 24 * time.Hour

func (w *ScorePostingWorker) Work(ctx context.Context, job *river.Job[ScorePostingArgs]) error {
	posting, err := store.PostingForScoring(ctx, w.Deps.Pool, job.Args.PostingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // closed or deleted between enqueue and run
	}
	if err != nil {
		return err
	}

	profiles, err := w.candidateProfiles(ctx, posting)
	if err != nil {
		return err
	}

	if err := upsertScores(ctx, w.Deps, posting, profiles, w.Scorer); err != nil {
		return err
	}

	// Drop score rows this run did not write.
	//
	// Candidate selection narrows by geography and recent activity, so a user
	// who changes their country preference — or simply stops logging in — leaves
	// behind a row that will never be selected again. Without this, that row is
	// immortal: it keeps its old score forever, and the stale-version sweep
	// re-enqueues this posting every five minutes trying to fix a row it can
	// never reach. The sweep would never converge.
	//
	// Everyone still eligible was just written with the current version, so
	// anything left on an older version is by definition no longer applicable.
	// Deleting it is also the correct answer on its own terms: a score the
	// current rules would not produce should not be shown.
	if err := store.PruneSupersededScores(ctx, w.Deps.Pool, posting.ID, w.Scorer.Version()); err != nil {
		return err
	}

	if len(profiles) == 0 {
		return nil
	}

	// The fan-out ratio is a DERIVED estimate, not a measurement — and it was
	// wrong by 4x once already. Recording it per job is how it stops being a
	// guess the capacity model silently depends on.
	w.Deps.Log.DebugContext(ctx, "posting scored",
		"posting_id", posting.ID, "users", len(profiles))
	return nil
}

// candidateProfiles selects users this posting could plausibly suit.
func (w *ScorePostingWorker) candidateProfiles(ctx context.Context, j matching.Posting) ([]matching.Profile, error) {
	return store.CandidateProfiles(ctx, w.Deps.Pool, activeWindow.String(), j.Mode, j.Country)
}

// ScoreUserWorker rescores every live posting for one user.
//
// Runs on the low-priority queue: a full rescore after onboarding must never
// starve live scoring of newly-ingested postings.
type ScoreUserWorker struct {
	river.WorkerDefaults[ScoreUserArgs]
	Deps   *Deps
	Scorer *matching.Scorer
}

func (w *ScoreUserWorker) Work(ctx context.Context, job *river.Job[ScoreUserArgs]) error {
	profile, err := store.ProfileForScoring(ctx, w.Deps.Pool, job.Args.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	// Cap the work. Storing every score for every user is what makes
	// user_job_scores the fastest-growing table; the top N by recency is what
	// the user will actually page through.
	const cap = 2000

	postings, err := store.PostingsForScoring(ctx, w.Deps.Pool, cap)
	if err != nil {
		return err
	}

	var (
		scored int
		batch  = make([]store.ScoreRow, 0, scoreFlushSize)
	)
	for _, j := range postings {
		row, err := newScoreRow(profile.UserID, j, w.Scorer.Score(profile, j))
		if err != nil {
			return err
		}
		batch = append(batch, row)
		if len(batch) == scoreFlushSize {
			if err := store.UpsertScores(ctx, w.Deps.Pool, batch, w.Scorer.Version()); err != nil {
				return err
			}
			batch = batch[:0]
		}
		scored++
	}
	if err := store.UpsertScores(ctx, w.Deps.Pool, batch, w.Scorer.Version()); err != nil {
		return err
	}

	w.Deps.Log.InfoContext(ctx, "user rescored", "user_id", profile.UserID, "postings", scored)
	return nil
}

// --- shared helpers ---------------------------------------------------------

// profile_version is stamped on every row by store.UpsertScores, so we can
// always answer "which model produced this?" Without it a weight change
// silently produces a feed mixing old and new scores with no way to tell them
// apart.

// scoreFlushSize bounds one statement's payload.
//
// The saving is round trips, and collapsing 500 into 1 already takes that cost
// to nothing; a larger batch would only grow the JSON document, which is held
// in memory twice — once in Go, once in the server's parse. A user rescore
// sweeps the whole live corpus, so this must be bounded by something.
const scoreFlushSize = 500

func newScoreRow(userID int64, posting matching.Posting, r matching.Result) (store.ScoreRow, error) {
	components, err := json.Marshal(r.Components)
	if err != nil {
		return store.ScoreRow{}, fmt.Errorf("marshal components user=%d posting=%d: %w",
			userID, posting.ID, err)
	}
	return store.ScoreRow{
		UserID:        userID,
		PostingID:     posting.ID,
		Score:         float64(r.Score),
		Band:          string(r.Band),
		Components:    components,
		Confidence:    float64(r.Confidence),
		MissingSkills: missingSkills(r),
	}, nil
}

// flushScores writes a batch of scores in a single statement.
//
// Every caller here fans out — one posting across every active user, or one
// user across the whole live corpus — so a per-row round trip makes the cost of
// scoring linear in network latency rather than in computation. It was exactly
// that: 32 seconds to score one posting for 62 users, ten workers, 14,700 jobs
// queued on the interactive queue, and GET /v1/me/dashboard timing out at ten
// seconds behind them.
//
// jsonb_to_recordset rather than unnest() because missing_skills is itself an
// array per row, and Postgres arrays are rectangular: a ragged array of arrays
// has no unnest form, while JSON carries it exactly.

// upsertScores writes every score for one posting in a single statement.
//
// This was one Exec per user inside a loop. Each is a round trip, so the cost
// of scoring a posting was linear in the number of candidates *in network
// latency* rather than in computation: measured at 32s per posting for 62
// users, with ten workers, which put 14,700 jobs on the live queue and left
// GET /v1/me/dashboard timing out at ten seconds. Fan-out is the whole shape of
// this workload — one posting is scored against every active user — so it is
// the one place a per-row round trip must not appear.
//
// jsonb_to_recordset rather than unnest() because missing_skills is itself an
// array per row, and Postgres arrays are rectangular: a ragged array of arrays
// has no unnest form, while JSON carries it exactly.
func upsertScores(ctx context.Context, d *Deps, posting matching.Posting,
	profiles []matching.Profile, scorer *matching.Scorer) error {
	rows := make([]store.ScoreRow, 0, len(profiles))
	for _, p := range profiles {
		row, err := newScoreRow(p.UserID, posting, scorer.Score(p, posting))
		if err != nil {
			return err
		}
		rows = append(rows, row)
		if len(rows) == scoreFlushSize {
			if err := store.UpsertScores(ctx, d.Pool, rows, scorer.Version()); err != nil {
				return err
			}
			rows = rows[:0]
		}
	}
	return store.UpsertScores(ctx, d.Pool, rows, scorer.Version())
}

// missingSkills lifts the skills gap out of the component breakdown so the feed
// can render it without unnesting JSON per row.
//
// Always returns a non-nil slice: a NULL column means "not scored since this
// column existed", and a zero-length array means "nothing missing". Collapsing
// those two would make a perfect match indistinguishable from a stale row.
func missingSkills(r matching.Result) []string {
	for _, c := range r.Components {
		if c.Name == "skills" {
			if c.Missing == nil {
				return []string{}
			}
			return c.Missing
		}
	}
	return []string{}
}
