package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Application is one tracked role, at whatever stage it has reached.
type Application struct {
	ID          int64      `db:"id"`
	PostingID   *int64     `db:"posting_id"`
	CompanyName string     `db:"company_name"`
	RoleTitle   string     `db:"role_title"`
	Status      string     `db:"status"`
	ApplyURL    string     `db:"apply_url"`
	Note        string     `db:"note"`
	NextAction  string     `db:"next_action"`
	NextAt      *time.Time `db:"next_action_at"`
	AppliedAt   *time.Time `db:"applied_at"`
	UpdatedAt   time.Time  `db:"updated_at"`

	// DaysSinceActivity is a fact, not a verdict.
	//
	// It replaces a job that used to set status='ghosted' after 21 days.
	// Silence is not evidence of intent, and our records hold only what the
	// user typed — so closing an application on their behalf asserted something
	// we could not know.
	DaysSinceActivity int `db:"days_since_activity"`
}

// ApplicationPatch carries only the fields a caller asked to change. A nil
// field is "leave alone", which is why every one is a pointer.
type ApplicationPatch struct {
	Status     *string
	Channel    *string
	Note       *string
	NextAction *string
	NextAt     *string
}

// applicationColumns is the projection every application query returns, so one
// struct and one scan path serve the list, the insert and the update.
const applicationColumns = `
	       a.id,
	       a.posting_id,
	       COALESCE(a.company_name, '')  AS company_name,
	       COALESCE(a.role_title, '')    AS role_title,
	       a.status::text                AS status,
	       COALESCE(a.apply_url, '')     AS apply_url,
	       COALESCE(a.why_applied, '')   AS note,
	       COALESCE(a.next_action, '')   AS next_action,
	       a.next_action_at,
	       a.applied_at,
	       a.updated_at,
	       GREATEST(0, EXTRACT(day FROM now() - a.last_activity_at)::int) AS days_since_activity`

// ListApplications returns a user's tracked roles, most recently active first.
func ListApplications(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]Application, error) {
	rows, err := pool.Query(ctx, `
		SELECT`+applicationColumns+`
		  FROM applications a
		 WHERE a.user_id = $1
		 ORDER BY a.last_activity_at DESC
		 LIMIT 200`, userID)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[Application])
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	return out, nil
}

// SaveJob saves a posting for a user, idempotently.
//
// Company and title are denormalised on purpose: a tracked application must
// survive the posting being taken down, and it will be — that is what happens
// to every job eventually. Reading the title through a join would leave the
// user with a blank row for a role they actually interviewed for.
func SaveJob(ctx context.Context, pool *pgxpool.Pool, userID, postingID int64) (Application, error) {
	rows, err := pool.Query(ctx, `
		WITH a AS (
			INSERT INTO applications
				(user_id, posting_id, company_id, company_name, role_title, status, ats_vendor, apply_url)
			SELECT $1, p.id, p.company_id, c.name, p.title, 'saved', s.vendor, p.apply_url
			  FROM job_postings p
			  JOIN companies c ON c.id = p.company_id
			  JOIN sources s ON s.id = p.source_id
			 WHERE p.id = $2
			ON CONFLICT (user_id, posting_id) WHERE posting_id IS NOT NULL
			DO UPDATE SET last_activity_at = now()
			RETURNING *
		)
		SELECT`+applicationColumns+` FROM a`, userID, postingID)
	if err != nil {
		return Application{}, fmt.Errorf("save job: %w", err)
	}
	it, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Application])
	if errors.Is(err, pgx.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	if err != nil {
		return Application{}, fmt.Errorf("save job: %w", err)
	}
	return it, nil
}

// UnsaveJob removes a bare save.
//
// Only a bare save is removable. Once an application has advanced, deleting it
// would silently destroy interview history the user cannot reconstruct;
// withdrawing is the status change for that, and it is not this operation.
func UnsaveJob(ctx context.Context, pool *pgxpool.Pool, userID, postingID int64) error {
	tag, err := pool.Exec(ctx,
		`DELETE FROM applications WHERE user_id = $1 AND posting_id = $2 AND status = 'saved'`,
		userID, postingID)
	if err != nil {
		return fmt.Errorf("unsave job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotRemovable
	}
	return nil
}

// UpdateApplication advances an application through the funnel and records the
// transition.
//
// Three plain statements in a transaction, deliberately not one clever CTE. The
// first attempt read the old status in a `SELECT ... FOR UPDATE` CTE beside an
// `UPDATE` CTE and joined them. It compiled, ran, returned 204, and silently
// logged nothing: within one statement those two CTEs do not see each other's
// rows, so the join was empty every time.
func UpdateApplication(ctx context.Context, pool *pgxpool.Pool, userID, appID int64, p ApplicationPatch) error {
	return InTx(ctx, pool, func(tx pgx.Tx) error {
		// FOR UPDATE so a second concurrent PATCH cannot interleave between the
		// read and the write and produce an event claiming a transition that
		// never happened.
		var oldStatus string
		err := tx.QueryRow(ctx,
			`SELECT status::text FROM applications WHERE id = $1 AND user_id = $2 FOR UPDATE`,
			appID, userID).Scan(&oldStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read application: %w", err)
		}

		newStatus, err := applyPatch(ctx, tx, userID, appID, p)
		if err != nil {
			return err
		}

		// Only a real transition is an event.
		//
		// `application_events` existed from the first migration and NOTHING had
		// ever written to it, so "your activity" had no history to draw on and
		// would have rendered an empty grid forever while looking like a
		// working feature. Editing a note is not activity, and counting it
		// would inflate a record whose entire value is being an honest one.
		if newStatus == oldStatus {
			return nil
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO application_events (application_id, from_status, to_status, actor, note)
			VALUES ($1, $2::application_status, $3::application_status, 'user', $4)`,
			appID, oldStatus, newStatus, p.Note)
		if err != nil {
			return fmt.Errorf("record transition: %w", err)
		}
		return nil
	})
}

// applyPatch writes the changed fields and returns the resulting status.
//
// applied_at is set here rather than trusted from the client: it is the clock
// the ghost sweep and every funnel statistic depend on, and a client sending a
// wrong timestamp would corrupt both silently. COALESCE keeps the first value,
// so applied -> screen -> onsite does not keep resetting the date they applied.
func applyPatch(ctx context.Context, tx pgx.Tx, userID, appID int64, p ApplicationPatch) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `
		UPDATE applications SET
			status = COALESCE($3::application_status, status),
			why_applied = COALESCE($5, why_applied),
			next_action = COALESCE($6, next_action),
			next_action_at = COALESCE($7::date, next_action_at),
			applied_at = CASE
				WHEN $3 IS NOT NULL AND $3 NOT IN ('saved', 'withdrawn')
				THEN COALESCE(applied_at, now()) ELSE applied_at END,
			-- Advancing past 'saved' must satisfy applied_requires_attribution,
			-- so a channel is defaulted rather than letting the constraint
			-- reject a status change made from a UI that never asked.
			channel = CASE
				WHEN $3 IS NOT NULL AND $3 NOT IN ('saved', 'withdrawn')
				THEN COALESCE(NULLIF($4, '')::application_channel, channel, 'other')
				ELSE COALESCE(NULLIF($4, '')::application_channel, channel) END,
			last_activity_at = now(),
			updated_at = now()
		 WHERE id = $1 AND user_id = $2
		 RETURNING status::text`,
		appID, userID, p.Status, p.Channel, p.Note, p.NextAction, p.NextAt).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("update application: %w", err)
	}
	return status, nil
}

// ActivityDay is one cell of the activity grid.
type ActivityDay struct {
	Day   string `db:"day"` // YYYY-MM-DD, UTC
	Count int    `db:"count"`
}

// ActivityWindow is a bounded run of days with the total over it.
type ActivityWindow struct {
	Days  []ActivityDay
	From  string
	To    string
	Total int
}

// LoadActivity returns real movement over the last `weeks` weeks.
//
// Only days with activity are returned: the grid is 84 cells and a typical
// search touches a dozen, so sending 84 rows of mostly zeroes would be mostly
// zeroes. The client fills the gaps, which it must be able to do anyway for the
// days before the account existed.
//
// The window is aligned to Monday so the grid's rows mean the same thing every
// week; without that the top row drifts through the week and the shape stops
// being readable. Both bounds are computed in one statement with the counts, so
// a run spanning midnight cannot report days from one week against the bounds
// of another.
func LoadActivity(ctx context.Context, pool *pgxpool.Pool, userID int64, weeks int) (ActivityWindow, error) {
	var w ActivityWindow

	batch := &pgx.Batch{}
	batch.Queue(`
		SELECT to_char(d.day, 'YYYY-MM-DD') AS day, d.n AS count
		  FROM (
			SELECT e.occurred_at::date AS day, count(*) AS n
			  FROM application_events e
			  JOIN applications app ON app.id = e.application_id
			 WHERE app.user_id = $1
			   AND e.occurred_at::date >=
			       (date_trunc('week', now()) - make_interval(weeks => $2 - 1))::date
			 GROUP BY 1
		  ) d
		 ORDER BY d.day`, userID, weeks)
	batch.Queue(`
		SELECT to_char((date_trunc('week', now()) - make_interval(weeks => $1 - 1))::date, 'YYYY-MM-DD'),
		       to_char((date_trunc('week', now()) + interval '6 days')::date, 'YYYY-MM-DD')`, weeks)

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	days, err := collectBatch[ActivityDay](br)
	if err != nil {
		return w, fmt.Errorf("activity days: %w", err)
	}
	if err := br.QueryRow().Scan(&w.From, &w.To); err != nil {
		return w, fmt.Errorf("activity bounds: %w", err)
	}

	w.Days = days
	for _, d := range days {
		w.Total += d.Count
	}
	return w, nil
}
