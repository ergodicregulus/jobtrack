package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ergodicregulus/jobtrack/internal/httpx"
	"github.com/ergodicregulus/jobtrack/internal/store"
)

// savedItem is one tracked role on the wire.
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

	// DaysSinceActivity is a fact, not a verdict. See store.Application.
	DaysSinceActivity int `json:"days_since_activity"`
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

func (a *API) handleListSaved(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	items, err := store.ListApplications(ctx, a.pool, userID)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	out := mapSlice(items, func(v store.Application) savedItem { return savedItem(v) })
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

	it, err := store.SaveJob(ctx, a.pool, userID, postingID)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.ErrNotFound()
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, savedItem(it))
	return nil
}

func (a *API) handleUnsaveJob(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	postingID, err := pathInt(r, "id")
	if err != nil {
		return err
	}

	err = store.UnsaveJob(ctx, a.pool, userID, postingID)
	if errors.Is(err, store.ErrNotRemovable) {
		return httpx.ErrConflict("this application has progressed past saved; withdraw it instead of removing it")
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
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

	err = store.UpdateApplication(ctx, a.pool, userID, appID, store.ApplicationPatch(req))
	if errors.Is(err, store.ErrNotFound) {
		return httpx.ErrNotFound()
	}
	if err != nil {
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
