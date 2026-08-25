package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/store"
)

type dismissRequest struct {
	Reason string `json:"reason"`
}

func (a *API) handleListDismissals(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := store.ListDismissals(ctx, a.pool, httpx.UserIDFromContext(ctx), limit)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, map[string]any{"items": items})
	return nil
}

// handleDismiss hides a posting. PUT, because dismissing twice is the same fact.
//
// The body is optional: the feed's dismiss control sends none, and the detail
// page sends a reason. Requiring one would force the fast path to send
// `{"reason":null}` to say nothing.
func (a *API) handleDismiss(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	postingID, err := pathInt(r, "posting_id")
	if err != nil {
		return err
	}

	var req dismissRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		// An empty body is the common case and is not a client error. Only a
		// body that is present and malformed is.
		if r.ContentLength > 0 {
			return err
		}
	}

	err = store.Dismiss(ctx, a.pool, httpx.UserIDFromContext(ctx), postingID, req.Reason)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.ErrNotFound()
	}
	if err != nil {
		// The only user-correctable failure is an unknown reason; everything
		// else is ours.
		if req.Reason != "" && !contains(store.DismissReasons, req.Reason) {
			return httpx.ErrBadRequest("that is not a reason we recognise",
				httpx.FieldError{Field: "reason", Code: "enum", Message: "unknown reason"})
		}
		return httpx.ErrInternal(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleUndismiss restores a posting to the feed.
//
// Returns 204 whether or not a row existed. The user's intent is "this should
// not be hidden", and that is true afterwards either way — a 404 here would be
// reporting a database fact as a user error.
func (a *API) handleUndismiss(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	postingID, err := pathInt(r, "posting_id")
	if err != nil {
		return err
	}
	if err := store.Undismiss(ctx, a.pool, httpx.UserIDFromContext(ctx), postingID); err != nil {
		return httpx.ErrInternal(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
