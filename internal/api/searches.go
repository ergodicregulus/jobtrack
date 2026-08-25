package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/store"
)

// savedSearch is one stored filter on the wire.
type savedSearch struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Query     string     `json:"query"`
	IsDefault bool       `json:"is_default"`
	CreatedAt time.Time  `json:"created_at"`
	LastRunAt *time.Time `json:"last_run_at"`
	NewSince  int        `json:"new_since"`
}

type createSearchRequest struct {
	Name      string `json:"name"`
	Query     string `json:"query"`
	IsDefault bool   `json:"is_default"`
}

func (a *API) handleListSearches(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	items, err := store.ListSavedSearches(ctx, a.pool, httpx.UserIDFromContext(ctx))
	if err != nil {
		return httpx.ErrInternal(err)
	}
	out := mapSlice(items, func(v store.SavedSearch) savedSearch { return savedSearch(v) })
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, map[string]any{"items": out})
	return nil
}

func (a *API) handleCreateSearch(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	var req createSearchRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 80 {
		return httpx.ErrBadRequest("a saved search needs a name of 1 to 80 characters",
			httpx.FieldError{Field: "name", Code: "length", Message: "1-80 characters"})
	}
	// The leading `?` is accepted and stripped: it is what a caller has in hand
	// from location.search, and rejecting it would be pedantry rather than
	// validation.
	req.Query = strings.TrimPrefix(strings.TrimSpace(req.Query), "?")
	if len(req.Query) > 2000 {
		return httpx.ErrBadRequest("that filter is too long to save",
			httpx.FieldError{Field: "query", Code: "length", Message: "at most 2000 characters"})
	}

	s, err := store.CreateSavedSearch(ctx, a.pool, userID, req.Name, req.Query, req.IsDefault)
	switch {
	case errors.Is(err, store.ErrTooManySearches):
		return httpx.ErrConflict("you have reached the limit of saved searches; delete one first")
	case isUniqueViolation(err):
		return httpx.ErrConflict("you already have a saved search with that name")
	case err != nil:
		return httpx.ErrInternal(err)
	}

	w.Header().Set("Location", "/v1/me/searches")
	httpx.WriteJSON(ctx, w, a.log, http.StatusCreated, savedSearch(s))
	return nil
}

func (a *API) handleDeleteSearch(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	id, err := pathInt(r, "id")
	if err != nil {
		return err
	}
	err = store.DeleteSavedSearch(ctx, a.pool, httpx.UserIDFromContext(ctx), id)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.ErrNotFound()
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleRunSearch advances the new-since mark.
//
// Separate from listing on purpose: if merely rendering the sidebar cleared the
// marks, every badge would vanish the moment the page loaded and the feature
// would appear not to work.
func (a *API) handleRunSearch(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	id, err := pathInt(r, "id")
	if err != nil {
		return err
	}
	if err := store.MarkSavedSearchRun(ctx, a.pool, httpx.UserIDFromContext(ctx), id); err != nil {
		return httpx.ErrInternal(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// isUniqueViolation reports a Postgres 23505.
//
// Checked by code rather than by pre-querying for the name: a SELECT-then-
// INSERT is a race, and the unique index is the thing that actually decides.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
