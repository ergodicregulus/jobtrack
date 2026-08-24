package api

import (
	"net/http"
	"time"

	"github.com/jobtrack/jobtrack/internal/domain/user"
	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/store"
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

type MarketSummary struct {
	LivePostings  int     `json:"live_postings"`
	Companies     int     `json:"companies"`
	AddedThisWeek int     `json:"added_this_week"`
	RemoteShare   float64 `json:"remote_share"`
}

// ActivityDay is one cell of the activity grid.
type ActivityDay struct {
	Day   string `json:"day"` // YYYY-MM-DD, UTC
	Count int    `json:"count"`
}

// Activity is the last 12 weeks of real movement.
type Activity struct {
	Days []ActivityDay `json:"days"`
	// From and To bound the window the client should render, so the grid's
	// shape is decided in one place rather than recomputed from today's date in
	// the browser and drifting across a midnight boundary.
	From string `json:"from"`
	To   string `json:"to"`
	// Total is the sum over the window. Shown as a plain count, never as a
	// streak: this is a record of what happened, not a target to hit.
	Total int `json:"total"`
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
	d, err := store.LoadDashboard(ctx, a.pool, userID)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, Dashboard{
		Greeting:   profile.DisplayName(),
		Strength:   profile.Strength(),
		Matches:    MatchSummary(d.Matches),
		Market:     MarketSummary(d.Market),
		Pipeline:   pipelineStages(d.Pipeline),
		NeedsReply: mapSlice(d.NeedsReply, func(v store.ActionItem) ActionItem { return ActionItem(v) }),
		TopMatches: mapSlice(d.TopMatches, func(v store.TopMatch) DashboardJob { return DashboardJob(v) }),
	})
	return nil
}

// pipelineStages renders the funnel in a fixed order, including empty stages.
// A funnel that hides its zeroes reflows as the user progresses and stops being
// readable as a funnel at all.
func pipelineStages(counts map[string]int) []PipelineStage {
	out := make([]PipelineStage, 0, len(pipelineOrder))
	for _, st := range pipelineOrder {
		out = append(out, PipelineStage{Status: st.Status, Label: st.Label, Count: counts[st.Status]})
	}
	return out
}

// mapSlice converts store row types to their wire equivalents.
//
// The two are kept separate because api/openapi.yaml is the contract and the
// row shape is not. Today they coincide, and this conversion is where they are
// allowed to stop coinciding without breaking a client.
func mapSlice[In, Out any](in []In, f func(In) Out) []Out {
	out := make([]Out, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
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

	m, err := store.LoadMarketSummary(ctx, a.pool)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// Public and identical for every caller, so it caches. Sixty seconds is
	// well inside the ingest cadence — nobody sees a figure that is wrong by
	// more than one crawl.
	w.Header().Set("Cache-Control", "public, max-age=60")
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, MarketSummary(m))
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

	// 12 columns of 7 days, ending on the current week.
	const weeks = 12

	win, err := store.LoadActivity(ctx, a.pool, userID, weeks)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, Activity{
		Days:  mapSlice(win.Days, func(v store.ActivityDay) ActivityDay { return ActivityDay(v) }),
		From:  win.From,
		To:    win.To,
		Total: win.Total,
	})
	return nil
}
