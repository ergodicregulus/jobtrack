package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jobtrack/jobtrack/internal/domain/user"
	"github.com/jobtrack/jobtrack/internal/httpx"
)

// Dashboard is what a user sees on login.
//
// The widget set answers, in order, the four questions someone opening a job
// tracker actually has: is anything new for me, what needs a reply, how am I
// doing, and where is the market moving. Anything that answered none of those
// was cut — a dashboard that shows numbers nobody acts on trains people to
// ignore it.
type Dashboard struct {
	Greeting   string          `json:"greeting"`
	Matches    MatchSummary    `json:"matches"`
	Pipeline   []PipelineStage `json:"pipeline"`
	NeedsReply []ActionItem    `json:"needs_reply"`
	TopMatches []DashboardJob  `json:"top_matches"`
	Market     MarketSummary   `json:"market"`
	Strength   user.Strength   `json:"strength"`
}

type MatchSummary struct {
	Strong    int `json:"strong"`
	Plausible int `json:"plausible"`
	NewToday  int `json:"new_today"`
	Scored    int `json:"scored"`
}

type PipelineStage struct {
	Status string `json:"status"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
}

type ActionItem struct {
	ID          int64      `json:"id"`
	CompanyName string     `json:"company_name"`
	RoleTitle   string     `json:"role_title"`
	Status      string     `json:"status"`
	DaysSince   int        `json:"days_since"`
	NextAction  string     `json:"next_action,omitempty"`
	NextAt      *time.Time `json:"next_action_at,omitempty"`
}

type DashboardJob struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	CompanyName string    `json:"company_name"`
	Location    string    `json:"location"`
	Mode        string    `json:"mode"`
	Score       float64   `json:"score"`
	Band        string    `json:"band"`
	Missing     []string  `json:"missing_skills"`
	PostedAt    time.Time `json:"posted_at"`
	ApplyURL    string    `json:"apply_url"`
	Saved       bool      `json:"saved"`
}

// marketSummarySQL is the one definition of the corpus figures, shared by the
// dashboard and the public landing page so the two can never quote different
// numbers for the same thing.
//
// posted_at, not first_seen_at. first_seen_at records when WE fetched a
// posting, so after a backfill every row in the database looks like it arrived
// this week — which turned "added this week" into a restatement of the corpus
// size and the dashboard into a liar.
const marketSummarySQL = `
	SELECT count(*),
	       count(DISTINCT company_id),
	       count(*) FILTER (
	           WHERE COALESCE(posted_at, first_seen_at) > now() - interval '7 days'
	       ),
	       COALESCE(avg((mode = 'remote')::int), 0)
	  FROM job_postings WHERE status = 'live'`

// ActivityDay is one cell of the activity grid.
//
// Only days with activity are returned. The grid is 84 cells and a typical
// search touches a dozen of them, so sending 84 rows of mostly zeroes would be
// mostly zeroes — the client fills the gaps, which it has to be able to do
// anyway for the days before the account existed.
type ActivityDay struct {
	Day   string `json:"day"` // YYYY-MM-DD, UTC
	Count int    `json:"count"`
}

// Activity is the last 12 weeks of real movement.
type Activity struct {
	Days []ActivityDay `json:"days"`
	// From and To bound the window the client should render, so the grid's
	// shape is decided in one place rather than recomputed from today's date
	// in the browser and drifting across a midnight boundary.
	From string `json:"from"`
	To   string `json:"to"`
	// Total is the sum over the window. Shown as a plain count, never as a
	// streak: this is a record of what happened, not a target to hit.
	Total int `json:"total"`
}

type MarketSummary struct {
	LivePostings  int     `json:"live_postings"`
	Companies     int     `json:"companies"`
	AddedThisWeek int     `json:"added_this_week"`
	RemoteShare   float64 `json:"remote_share"`
}

// pipelineOrder is the funnel as a user experiences it. Terminal states are
// deliberately excluded from the widget: a column of rejections is not
// something anyone needs on their homepage every morning.
var pipelineOrder = []struct{ Status, Label string }{
	{"saved", "Saved"},
	{"applied", "Applied"},
	{"recruiter_screen", "Recruiter"},
	{"hm_screen", "Hiring manager"},
	{"onsite", "Onsite"},
	{"offer", "Offer"},
}

func (a *API) handleDashboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	profile, err := loadProfile(ctx, a.pool, userID)
	if err != nil {
		return err
	}

	var d Dashboard
	d.Greeting = profile.DisplayName()
	d.Strength = profile.Strength()

	// One round trip per widget would be five, and they are independent. They
	// run as a single batch instead: pgx pipelines them over one connection, so
	// the dashboard costs one network round trip rather than five.
	batch := &pgx.Batch{}

	// Three queries, not one, and the reason is the shape of the table.
	//
	// These four numbers used to be four `count(*) FILTER (...)` aggregates over
	// a single join, which meant reading every one of a user's ~15,000 score
	// rows to evaluate a filter that matches a few hundred of them. On a table
	// rewritten as often as this one — every rescore replaces every row a user
	// has — the heap is never clean enough for an index-only scan to hold, so
	// that read degraded to 12,149 heap fetches and 13.7 seconds, and the
	// dashboard returned 500 at its ten-second deadline.
	//
	// Split, each part reads only what it needs: 21ms + 79ms + 285ms measured
	// 2026-08-24, against 3,565ms fused on the same data.
	//
	// They stay in the same batch, so this is still one network round trip.

	// Bands: a few hundred rows, found by range scan rather than by filtering
	// the whole set. No join — a score row for a non-live posting is collected
	// by the maintenance sweep, so the join was only ever a safety net here and
	// it cost the whole scan.
	batch.Queue(`
		SELECT count(*) FILTER (WHERE band = 'strong'),
		       count(*) FILTER (WHERE band = 'plausible')
		  FROM user_job_scores
		 WHERE user_id = $1 AND band IN ('strong', 'plausible')`, userID)

	// New today: driven from the postings side, where freshness is indexed and
	// the population is small, then probed into the scores by primary key.
	batch.Queue(`
		SELECT count(*)
		  FROM job_postings p
		  JOIN user_job_scores s ON s.posting_id = p.id AND s.user_id = $1
		 WHERE p.status = 'live'
		   AND COALESCE(p.posted_at, p.first_seen_at) > now() - interval '24 hours'`, userID)

	// Total scored. This one is genuinely proportional to the corpus and there
	// is no honest way to make it cheaper than counting — an estimate would be
	// a fabricated number on a stat tile.
	batch.Queue(`SELECT count(*) FROM user_job_scores WHERE user_id = $1`, userID)

	batch.Queue(`
		SELECT status::text, count(*)
		  FROM applications WHERE user_id = $1
		 GROUP BY status`, userID)

	batch.Queue(`
		SELECT id, COALESCE(company_name, ''), COALESCE(role_title, ''), status::text,
		       GREATEST(0, EXTRACT(day FROM now() - last_activity_at)::int),
		       COALESCE(next_action, ''), next_action_at
		  FROM applications
		 WHERE user_id = $1
		   AND status IN ('applied','recruiter_screen','hm_screen','onsite')
		   AND (next_action_at <= current_date OR last_activity_at < now() - interval '7 days')
		 ORDER BY next_action_at NULLS LAST, last_activity_at
		 LIMIT 5`, userID)

	batch.Queue(`
		SELECT p.id, p.title, c.name, COALESCE(p.location_raw, ''), COALESCE(p.mode::text, ''),
		       s.score, s.band, COALESCE(s.missing_skills, '{}'),
		       COALESCE(p.posted_at, p.first_seen_at), COALESCE(p.apply_url, ''),
		       EXISTS (SELECT 1 FROM applications a WHERE a.user_id = $1 AND a.posting_id = p.id)
		  FROM user_job_scores s
		  JOIN job_postings p ON p.id = s.posting_id
		  JOIN companies c ON c.id = p.company_id
		 WHERE s.user_id = $1 AND p.status = 'live'
		 ORDER BY s.score DESC, p.first_seen_at DESC
		 LIMIT 6`, userID)

	batch.Queue(marketSummarySQL)

	br := a.pool.SendBatch(ctx, batch)
	defer br.Close()

	if err := br.QueryRow().Scan(&d.Matches.Strong, &d.Matches.Plausible); err != nil {
		return httpx.ErrInternal(err)
	}
	if err := br.QueryRow().Scan(&d.Matches.NewToday); err != nil {
		return httpx.ErrInternal(err)
	}
	if err := br.QueryRow().Scan(&d.Matches.Scored); err != nil {
		return httpx.ErrInternal(err)
	}

	counts := map[string]int{}
	rows, err := br.Query()
	if err != nil {
		return httpx.ErrInternal(err)
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return httpx.ErrInternal(err)
		}
		counts[status] = n
	}
	rows.Close()

	// Every stage is returned, including empty ones. A funnel that hides its
	// zeroes reflows as the user progresses and stops being readable as a
	// funnel at all.
	d.Pipeline = make([]PipelineStage, 0, len(pipelineOrder))
	for _, st := range pipelineOrder {
		d.Pipeline = append(d.Pipeline, PipelineStage{Status: st.Status, Label: st.Label, Count: counts[st.Status]})
	}

	d.NeedsReply = []ActionItem{}
	rows, err = br.Query()
	if err != nil {
		return httpx.ErrInternal(err)
	}
	for rows.Next() {
		var it ActionItem
		if err := rows.Scan(&it.ID, &it.CompanyName, &it.RoleTitle, &it.Status,
			&it.DaysSince, &it.NextAction, &it.NextAt); err != nil {
			rows.Close()
			return httpx.ErrInternal(err)
		}
		d.NeedsReply = append(d.NeedsReply, it)
	}
	rows.Close()

	d.TopMatches = []DashboardJob{}
	rows, err = br.Query()
	if err != nil {
		return httpx.ErrInternal(err)
	}
	for rows.Next() {
		var j DashboardJob
		if err := rows.Scan(&j.ID, &j.Title, &j.CompanyName, &j.Location, &j.Mode,
			&j.Score, &j.Band, &j.Missing, &j.PostedAt, &j.ApplyURL, &j.Saved); err != nil {
			rows.Close()
			return httpx.ErrInternal(err)
		}
		d.TopMatches = append(d.TopMatches, j)
	}
	rows.Close()

	if err := br.QueryRow().Scan(&d.Market.LivePostings, &d.Market.Companies,
		&d.Market.AddedThisWeek, &d.Market.RemoteShare); err != nil {
		return httpx.ErrInternal(err)
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, d)
	return nil
}

// --- Tracking ---

type savedItem struct {
	ID          int64      `json:"id"`
	PostingID   *int64     `json:"posting_id"`
	CompanyName string     `json:"company_name"`
	RoleTitle   string     `json:"role_title"`
	Status      string     `json:"status"`
	ApplyURL    string     `json:"apply_url,omitempty"`
	Note        string     `json:"note,omitempty"`
	NextAction  string     `json:"next_action,omitempty"`
	NextAt      *time.Time `json:"next_action_at,omitempty"`
	AppliedAt   *time.Time `json:"applied_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// DaysSinceActivity is a fact, not a verdict.
	//
	// It replaces a job that used to set status='ghosted' after 21 days. Silence
	// is not evidence of intent, and our records hold only what the user typed —
	// so closing an application on their behalf was asserting something we could
	// not know. The number is reported; what it means is theirs to decide.
	DaysSinceActivity int `json:"days_since_activity"`
}

func (a *API) handleListSaved(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	rows, err := a.pool.Query(ctx, `
		SELECT a.id, a.posting_id, COALESCE(a.company_name, ''), COALESCE(a.role_title, ''),
		       a.status::text, COALESCE(a.apply_url, ''), COALESCE(a.why_applied, ''),
		       COALESCE(a.next_action, ''), a.next_action_at, a.applied_at, a.updated_at,
		       GREATEST(0, EXTRACT(day FROM now() - a.last_activity_at)::int)
		  FROM applications a
		 WHERE a.user_id = $1
		 ORDER BY a.last_activity_at DESC
		 LIMIT 200`, userID)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	defer rows.Close()

	out := []savedItem{}
	for rows.Next() {
		var it savedItem
		var nextAt *time.Time
		if err := rows.Scan(&it.ID, &it.PostingID, &it.CompanyName, &it.RoleTitle,
			&it.Status, &it.ApplyURL, &it.Note, &it.NextAction, &nextAt,
			&it.AppliedAt, &it.UpdatedAt, &it.DaysSinceActivity); err != nil {
			return httpx.ErrInternal(err)
		}
		it.NextAt = nextAt
		out = append(out, it)
	}
	if rows.Err() != nil {
		return httpx.ErrInternal(rows.Err())
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, map[string]any{"items": out})
	return nil
}

// handleSaveJob saves a posting, idempotently.
//
// PUT rather than POST because saving the same job twice must be the same as
// saving it once — the button is easy to double-click and the result should
// never be two rows.
func (a *API) handleSaveJob(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	postingID, err := pathInt(r, "id")
	if err != nil {
		return err
	}

	// Company and title are denormalised on purpose: a tracked application must
	// survive the posting being taken down, and it will be — that is what
	// happens to every job eventually. Reading the title through a join would
	// leave the user with a blank row for a role they actually interviewed for.
	var it savedItem
	err = a.pool.QueryRow(ctx, `
		INSERT INTO applications (user_id, posting_id, company_id, company_name, role_title, status, ats_vendor, apply_url)
		SELECT $1, p.id, p.company_id, c.name, p.title, 'saved', s.vendor, p.apply_url
		  FROM job_postings p
		  JOIN companies c ON c.id = p.company_id
		  JOIN sources s ON s.id = p.source_id
		 WHERE p.id = $2
		ON CONFLICT (user_id, posting_id) WHERE posting_id IS NOT NULL
		DO UPDATE SET last_activity_at = now()
		RETURNING id, posting_id, company_name, role_title, status::text,
		          COALESCE(apply_url, ''), COALESCE(why_applied, ''), updated_at`,
		userID, postingID).
		Scan(&it.ID, &it.PostingID, &it.CompanyName, &it.RoleTitle, &it.Status,
			&it.ApplyURL, &it.Note, &it.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound() // no such posting
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, it)
	return nil
}

func (a *API) handleUnsaveJob(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	postingID, err := pathInt(r, "id")
	if err != nil {
		return err
	}

	// Only a bare save is removable. Once an application has advanced, deleting
	// it would silently destroy interview history the user cannot reconstruct;
	// withdrawing is the status change for that, and it is not this endpoint.
	tag, err := a.pool.Exec(ctx,
		`DELETE FROM applications WHERE user_id = $1 AND posting_id = $2 AND status = 'saved'`,
		userID, postingID)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrConflict("this application has progressed past saved; withdraw it instead of removing it")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

type updateSavedRequest struct {
	Status     *string `json:"status,omitempty"`
	Channel    *string `json:"channel,omitempty"`
	Note       *string `json:"note,omitempty"`
	NextAction *string `json:"next_action,omitempty"`
	NextAt     *string `json:"next_action_at,omitempty"`
}

var validStatus = map[string]bool{
	"saved": true, "applied": true, "referred": true, "recruiter_screen": true,
	"hm_screen": true, "onsite": true, "offer": true, "rejected": true,
	"ghosted": true, "withdrawn": true,
}

var validChannel = map[string]bool{
	"careers_page": true, "job_board": true, "referral": true,
	"cold_email": true, "recruiter_inbound": true, "other": true,
}

// handleUpdateSaved advances an application through the funnel.
func (a *API) handleUpdateSaved(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	appID, err := pathInt(r, "id")
	if err != nil {
		return err
	}

	var req updateSavedRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.Status != nil && !validStatus[*req.Status] {
		return httpx.ErrBadRequest("unknown status " + strconv.Quote(*req.Status))
	}
	if req.Channel != nil && *req.Channel != "" && !validChannel[*req.Channel] {
		return httpx.ErrBadRequest("unknown channel " + strconv.Quote(*req.Channel))
	}

	// applied_at is set here rather than trusted from the client: it is the
	// clock the ghost sweep and every funnel statistic depend on, and a client
	// that sends a wrong timestamp would corrupt both silently. COALESCE keeps
	// the first value, so moving applied -> screen -> onsite does not keep
	// resetting the date the user actually applied.
	// A transaction with three plain steps, deliberately not one clever CTE.
	//
	// The first attempt read the old status in a `SELECT ... FOR UPDATE` CTE
	// beside an `UPDATE` CTE and joined them. It compiled, ran, returned 204,
	// and silently logged nothing: within one statement those two CTEs do not
	// see each other's rows, so the join was empty every time. Three readable
	// statements in a transaction are worth more than one that is subtly wrong.
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	defer tx.Rollback(ctx)

	// FOR UPDATE so a second concurrent PATCH cannot interleave between the
	// read and the write and produce an event claiming a transition that never
	// happened.
	var oldStatus string
	err = tx.QueryRow(ctx,
		`SELECT status::text FROM applications WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		appID, userID).Scan(&oldStatus)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return httpx.ErrNotFound()
	case err != nil:
		return httpx.ErrInternal(err)
	}

	// applied_at is set here rather than trusted from the client: it is the
	// clock every funnel statistic depends on, and a client sending a wrong
	// timestamp would corrupt them silently. COALESCE keeps the first value, so
	// applied -> screen -> onsite does not keep resetting the date they applied.
	var newStatus string
	if err := tx.QueryRow(ctx, `
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
		appID, userID, req.Status, req.Channel, req.Note, req.NextAction, req.NextAt).
		Scan(&newStatus); err != nil {
		return httpx.ErrInternal(err)
	}

	// Only a real transition is an event.
	//
	// `application_events` existed from the first migration and NOTHING had
	// ever written to it, so "your activity" had no history to draw on and
	// would have rendered an empty grid forever while looking like a working
	// feature. Editing a note is not activity, and counting it would inflate a
	// record whose entire value is being an honest one.
	if newStatus != oldStatus {
		if _, err := tx.Exec(ctx, `
			INSERT INTO application_events (application_id, from_status, to_status, actor, note)
			VALUES ($1, $2::application_status, $3::application_status, 'user', $4)`,
			appID, oldStatus, newStatus, req.Note); err != nil {
			return httpx.ErrInternal(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return httpx.ErrInternal(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func pathInt(r *http.Request, name string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || v <= 0 {
		return 0, httpx.ErrBadRequest(name + " must be a positive integer")
	}
	return v, nil
}

// handleMarket serves the corpus figures to anyone, signed in or not.
//
// The landing page quotes real numbers rather than "thousands of
// opportunities", and this is where they come from. It previously derived a
// "companies" figure client-side by counting the keys of the countries facet,
// which is a count of COUNTRIES with a company's label on it — a false
// statistic that had simply not been rendered yet.
func (a *API) handleMarket(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	var m MarketSummary
	if err := a.pool.QueryRow(ctx, marketSummarySQL).Scan(
		&m.LivePostings, &m.Companies, &m.AddedThisWeek, &m.RemoteShare); err != nil {
		return httpx.ErrInternal(err)
	}

	// Public and identical for every caller, so it caches. Sixty seconds is
	// well inside the ingest cadence — nobody sees a figure that is wrong by
	// more than one crawl.
	w.Header().Set("Cache-Control", "public, max-age=60")
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, m)
	return nil
}

// handleActivity returns the last 12 weeks of application activity.
//
// "Activity" is deliberately narrow: an application sent, or a status that
// genuinely changed. Not logins, not searches, not saves. A grid that lights up
// because someone opened the page would be measuring attendance, and this
// audience would spot that immediately — the value of the widget is that every
// filled cell corresponds to something real that happened in a hiring process.
func (a *API) handleActivity(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	// 12 columns of 7 days, ending on the current week. Aligned to Monday so
	// the grid's rows mean the same thing every week; without the alignment the
	// top row drifts through the week and the shape stops being readable.
	const weeks = 12

	rows, err := a.pool.Query(ctx, `
		WITH bounds AS (
			SELECT (date_trunc('week', now()) - make_interval(weeks => $2 - 1))::date AS from_day,
			       (date_trunc('week', now()) + interval '6 days')::date AS to_day
		)
		SELECT to_char(d.day, 'YYYY-MM-DD'), d.n
		  FROM (
			SELECT e.occurred_at::date AS day, count(*) AS n
			  FROM application_events e
			  JOIN applications app ON app.id = e.application_id
			 WHERE app.user_id = $1
			   AND e.occurred_at::date >= (SELECT from_day FROM bounds)
			 GROUP BY 1
		  ) d
		 ORDER BY d.day`, userID, weeks)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	defer rows.Close()

	out := Activity{Days: []ActivityDay{}}
	for rows.Next() {
		var d ActivityDay
		if err := rows.Scan(&d.Day, &d.Count); err != nil {
			return httpx.ErrInternal(err)
		}
		out.Total += d.Count
		out.Days = append(out.Days, d)
	}
	if err := rows.Err(); err != nil {
		return httpx.ErrInternal(err)
	}

	if err := a.pool.QueryRow(ctx, `
		SELECT to_char((date_trunc('week', now()) - make_interval(weeks => $1 - 1))::date, 'YYYY-MM-DD'),
		       to_char((date_trunc('week', now()) + interval '6 days')::date, 'YYYY-MM-DD')`,
		weeks).Scan(&out.From, &out.To); err != nil {
		return httpx.ErrInternal(err)
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, out)
	return nil
}
