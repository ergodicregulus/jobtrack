package api

import (
	"net/http"
	"sort"
	"strconv"
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

// IngestSeries is the homepage chart: what the corpus did, per vendor, per day.
type IngestSeries struct {
	Days   []string             `json:"days"`
	Series []IngestVendorSeries `json:"series"`
}

// IngestVendorSeries is one line on the chart.
type IngestVendorSeries struct {
	Vendor string `json:"vendor"`
	// New is the flow — postings first seen that day.
	New []int `json:"new"`
	// Live is the level — postings still live at the end of that day. Both are
	// charted because they answer different questions: a board can be adding
	// steadily while shrinking overall.
	Live []int `json:"live"`
}

// handleIngestSeries serves the corpus chart from the daily rollup.
//
// Aggregated to vendor rather than to source. Sixty-five lines is not a chart
// anyone can read; three to eight is. The rollup keeps per-source grain so a
// per-company view remains possible without a schema change.
func (a *API) handleIngestSeries(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 365 {
			return httpx.ErrBadRequest("days must be between 1 and 365",
				httpx.FieldError{Field: "days", Code: "range", Message: "1-365"})
		}
		days = n
	}

	rows, err := store.SourceDailySeries(ctx, a.pool, days)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// Public and identical for every caller, and the rollup only moves hourly.
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, foldByVendor(rows))
	return nil
}

// foldByVendor turns per-source rows into one aligned series per vendor.
//
// Every vendor gets a value for every day in the window, including zero. A gap
// and a zero look different on a chart and mean different things — "we saw
// nothing" is a fact, "we have no data" is an absence — and a client cannot
// tell them apart unless the zero is actually sent.
func foldByVendor(rows []store.SourceDay) IngestSeries {
	var days []string
	index := map[string]int{}
	for _, r := range rows {
		d := r.Day.Format("2006-01-02")
		if _, seen := index[d]; !seen {
			index[d] = len(days)
			days = append(days, d)
		}
	}

	byVendor := map[string]*IngestVendorSeries{}
	for _, r := range rows {
		s, ok := byVendor[r.Vendor]
		if !ok {
			s = &IngestVendorSeries{
				Vendor: r.Vendor,
				New:    make([]int, len(days)),
				Live:   make([]int, len(days)),
			}
			byVendor[r.Vendor] = s
		}
		i := index[r.Day.Format("2006-01-02")]
		s.New[i] += r.PostingsNew
		s.Live[i] += r.PostingsLive
	}

	out := IngestSeries{Days: days, Series: make([]IngestVendorSeries, 0, len(byVendor))}
	for _, s := range byVendor {
		out.Series = append(out.Series, *s)
	}
	// Stable order so the chart's colours do not shuffle between requests.
	sort.Slice(out.Series, func(i, j int) bool { return out.Series[i].Vendor < out.Series[j].Vendor })
	return out
}
