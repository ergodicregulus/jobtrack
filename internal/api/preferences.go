package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jobtrack/jobtrack/internal/domain/user"
	"github.com/jobtrack/jobtrack/internal/httpx"
)

// ThemeCookieName carries the resolved theme for server-side rendering.
//
// The database is the source of truth, but SSR needs the value BEFORE the page
// renders, and reading the database is not possible from the browser. A cookie
// is the only thing available to the server at render time, so the preference
// is mirrored into one on every write.
//
// Not HttpOnly: the client toggle updates it optimistically so the theme flips
// instantly rather than after a round trip. It carries no secret — the worst an
// attacker can do by setting it is choose your colour scheme.
const ThemeCookieName = "jt_theme"

func (a *API) handleGetPreferences(w http.ResponseWriter, r *http.Request) error {
	prefs, err := a.loadPreferences(r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, prefs)
	return nil
}

// handlePatchPreferences applies a partial update.
//
// Partial, not replace: a client that only knows about `theme` must not wipe
// `density` by omitting it. Unknown keys already in storage are preserved by
// the domain type, so an older release cannot discard a setting written by a
// newer one during a rolling deploy.
func (a *API) handlePatchPreferences(w http.ResponseWriter, r *http.Request) error {
	var update user.Update
	if err := decodeJSON(r, &update); err != nil {
		return err
	}

	prefs, err := a.loadPreferences(r)
	if err != nil {
		return err
	}
	if err := prefs.Apply(update); err != nil {
		return httpx.ErrBadRequest(err.Error())
	}

	userID := httpx.UserIDFromContext(r.Context())
	if _, err := a.pool.Exec(r.Context(),
		`UPDATE users SET preferences = $2, updated_at = now() WHERE id = $1`,
		userID, prefs); err != nil {
		return httpx.ErrInternal(err)
	}

	a.setThemeCookie(w, prefs.Theme)
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, prefs)
	return nil
}

func (a *API) loadPreferences(r *http.Request) (user.Preferences, error) {
	userID := httpx.UserIDFromContext(r.Context())

	var prefs user.Preferences
	err := a.pool.QueryRow(r.Context(),
		`SELECT preferences FROM users WHERE id = $1 AND deleted_at IS NULL`,
		userID).Scan(&prefs)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return prefs, httpx.ErrNotFound()
	case err != nil:
		return prefs, httpx.ErrInternal(err)
	}
	return prefs, nil
}

// setThemeCookie mirrors the stored preference for SSR.
func (a *API) setThemeCookie(w http.ResponseWriter, theme user.Theme) {
	http.SetCookie(w, &http.Cookie{
		Name:     ThemeCookieName,
		Value:    string(theme),
		Path:     "/",
		HttpOnly: false, // the client toggle updates it for an instant flip
		Secure:   a.cfg.Security.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
	})
}
