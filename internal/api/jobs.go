package api

import (
	"errors"
	"net/http"

	"github.com/ergodicregulus/jobtrack/internal/httpx"
	"github.com/ergodicregulus/jobtrack/internal/store"
)

// handleJobs serves the feed.
//
// Filtering, sorting and pagination all happen in Postgres. The client receives
// a page of ~25 already-decided results and renders them — shipping a query
// engine to the browser is what blows the interaction budget on a low-end
// laptop.
func (a *API) handleJobs(w http.ResponseWriter, r *http.Request) error {
	f, err := parseFeedFilter(r)
	if err != nil {
		return err
	}

	// The feed is public, but a signed-in viewer gets their match scores in the
	// same query. Optional rather than required auth: making discovery need an
	// account would hide the thing that demonstrates the product's value.
	if uid := httpx.UserIDFromContext(r.Context()); uid != 0 {
		f.UserID = &uid
	}

	// A band is a statement about one person's fit, so it cannot be honoured
	// without a profile to compare against. Rejecting is the only honest
	// response: silently ignoring the filter would return the unfiltered feed
	// under a heading promising strong matches, and returning nothing would
	// look like "you match nothing" rather than "we cannot know".
	if len(f.Bands) > 0 && f.UserID == nil {
		return httpx.ErrBadRequest("filtering by match band requires an account",
			httpx.FieldError{Field: "band", Code: "requires_auth",
				Message: "Sign in to filter by how well a role fits you"})
	}

	page, err := store.Feed(r.Context(), a.pool, a.scorer, f, r.URL.Query().Get("cursor"))
	if errors.Is(err, store.ErrRankingNeedsViewer) {
		return httpx.ErrBadRequest(
			"sorting by match or filtering by band needs an account — a band is a statement about your fit",
			httpx.FieldError{Field: "band", Code: "requires_auth", Message: "sign in to rank by match"})
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// The marker for "new since your last visit". Read separately from the feed
	// query because it belongs to the VIEWER, not to any posting, and folding it
	// into the row query would repeat one timestamp across every row.
	if f.UserID != nil {
		if since, err := store.PreviousVisit(r.Context(), a.pool, *f.UserID); err == nil {
			page.SinceLastVisit = since
		}
		// A failure here is not worth failing the feed over: the reader loses a
		// convenience, not the jobs.
	}

	// Personalised and freshness-critical, so never cached at the edge. A
	// cached feed is a stale feed, and freshness is the product.
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, page)
	return nil
}

// handleFacets returns counts per filter value.
func (a *API) handleFacets(w http.ResponseWriter, r *http.Request) error {
	facets, err := store.Facets(r.Context(), a.pool)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, facets)
	return nil
}

// parseFeedFilter reads the feed filter from the request's query string, and
// turns a bad parameter into a 400 that names it.
//
// The parsing is store.FeedFilterFromQuery, shared with saved searches and the
// digest, so all three read a filter the same way.
func parseFeedFilter(r *http.Request) (store.FeedFilter, error) {
	f, err := store.FeedFilterFromQuery(r.URL.Query())
	var fe *store.FilterError
	if errors.As(err, &fe) {
		return f, httpx.ErrBadRequest(fe.Detail,
			httpx.FieldError{Field: fe.Field, Code: fe.Code, Message: fe.Message})
	}
	return f, err
}
