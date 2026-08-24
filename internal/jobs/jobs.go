// Package jobs defines the background work River executes.
//
// Every job here obeys three rules, because workers are killed routinely by
// autoscalers and rolling deploys:
//
//   - Idempotent. A job that runs twice must produce the same result as once.
//   - Bounded. No unbounded loops; each run does a fixed amount of work.
//   - Self-describing. Args carry IDs, never PII — the job tables are less
//     protected than the encrypted columns.
package jobs

import (
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Queue names. Separate queues with separate concurrency limits are what stop
// a 20-million-row rescore from starving live scoring.
const (
	QueueIngest = "ingest"
	QueueEmbed  = "embed"
	QueueScore  = "score"
	QueueBulk   = "score_bulk"
	QueueMaint  = "maintenance"
)

// statesInFlight is the uniqueness window used by every job here: a duplicate
// is rejected while an identical job is still queued or working, and allowed
// once that job has finished.
//
// This deliberately departs from River's default, which also includes
// `completed`. The default is right for "this must happen at most once ever" —
// a signup email. Every job in this package is the opposite: recurring work
// whose whole purpose is to run again on the next tick. Including `completed`
// would mean a source fetched successfully could never be fetched again until
// the job cleaner eventually removed the row, which is a silent, hours-long
// ingestion stall that looks like a vendor problem.
//
// River requires available, pending, running and scheduled to be present;
// retryable is optional and included on purpose, so a job that failed and is
// waiting to retry still blocks a duplicate.
var statesInFlight = []rivertype.JobState{
	rivertype.JobStatePending,
	rivertype.JobStateScheduled,
	rivertype.JobStateAvailable,
	rivertype.JobStateRunning,
	rivertype.JobStateRetryable,
}

// FetchSourceArgs polls one source feed.
type FetchSourceArgs struct {
	SourceID int64 `json:"source_id"`
	// Force skips conditional-request validators. Used by an operator forcing a
	// refresh after fixing an adapter, never on the normal path.
	Force bool `json:"force,omitempty"`
}

func (FetchSourceArgs) Kind() string { return "fetch_source" }

func (FetchSourceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueIngest,
		// One in-flight fetch per source. Without this, a slow board plus a
		// fast scheduler tick would stack duplicate fetches and hammer the
		// vendor — the opposite of the politeness the whole design promises.
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: statesInFlight,
		},
		MaxAttempts: 5,
	}
}

// ScheduleSourcesArgs selects sources that are due and enqueues fetches.
//
// The scheduler runs this; it does no fetching itself. Separating "decide what
// is due" from "do the work" is what lets fetching scale to twenty replicas
// while the decision stays a singleton.
type ScheduleSourcesArgs struct {
	// Limit bounds one tick. A cold start with 5,000 due sources should trickle
	// rather than enqueue everything at once and saturate every worker.
	Limit int `json:"limit"`
}

func (ScheduleSourcesArgs) Kind() string { return "schedule_sources" }

func (ScheduleSourcesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMaint, MaxAttempts: 3}
}

// RetierSourcesArgs recomputes source tiers from observed change rate and user
// attention.
type RetierSourcesArgs struct{}

func (RetierSourcesArgs) Kind() string { return "retier_sources" }

func (RetierSourcesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMaint, MaxAttempts: 3}
}

// DedupeCompanyArgs runs deduplication across one company's live postings.
//
// Scoped per company because that is the blocking key: two postings can only be
// duplicates if they belong to the same company, so there is never a reason to
// compare across them.
type DedupeCompanyArgs struct {
	CompanyID int64 `json:"company_id"`
}

func (DedupeCompanyArgs) Kind() string { return "dedupe_company" }

func (DedupeCompanyArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueIngest,
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: statesInFlight,
			// Deduplication is not urgent and is expensive. Collapsing repeats
			// within a window keeps a busy company from re-running it on every
			// posting change.
			ByPeriod: 15 * time.Minute,
		},
		MaxAttempts: 3,
	}
}

// ScorePostingArgs scores one posting for the users it could plausibly suit.
//
// Fan-out is bounded inside the worker, not here: scoring every posting against
// every user is O(users x postings) and would dominate all other work.
type ScorePostingArgs struct {
	PostingID int64 `json:"posting_id"`
}

func (ScorePostingArgs) Kind() string { return "score_posting" }

func (ScorePostingArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueScore,
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: statesInFlight,
		},
		MaxAttempts: 3,
	}
}

// ScoreUserArgs rescores every live posting for one user.
//
// Enqueued when a user finishes onboarding or changes their profile — the two
// moments where every existing score is suddenly wrong.
type ScoreUserArgs struct {
	UserID int64 `json:"user_id"`
}

func (ScoreUserArgs) Kind() string { return "score_user" }

func (ScoreUserArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueBulk, // low priority: must never starve live scoring
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: statesInFlight,
		},
		MaxAttempts: 3,
	}
}

// RescoreStaleArgs re-scores rows produced by a superseded scoring version.
//
// This is what makes a scoring change actually take effect. Without it, a fix
// only reaches postings that happen to be re-ingested, and everything else keeps
// its old number forever — observed in development, where a corrected sales-role
// score stayed at 98% because nothing re-triggered it.
type RescoreStaleArgs struct {
	// Limit bounds one sweep. A version bump invalidates the entire corpus, and
	// re-scoring millions of rows in one transaction would be an outage; the
	// periodic schedule drains it over hours instead.
	Limit int `json:"limit"`
}

func (RescoreStaleArgs) Kind() string { return "rescore_stale" }

func (RescoreStaleArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMaint, MaxAttempts: 3}
}

// There is deliberately no ghost-sweep job.
//
// One used to set `status = 'ghosted'` after 21 days without movement, and it
// was wrong twice over. Silence is not evidence of intent — a hiring freeze, a
// delayed loop and a recruiter on leave all look identical to being ghosted.
// And our records contain only what the USER typed, so someone who got a reply
// and did not log it had their application closed by us, on the strength of
// missing data in our own database rather than in the world.
//
// "No movement for N days" is a fact and is computed live wherever it is shown.
// `ghosted` survives as a status a person can choose. Nothing writes a terminal
// status on a user's behalf.
