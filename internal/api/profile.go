package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/domain/user"
	"github.com/ergodicregulus/jobtrack/internal/httpx"
	"github.com/ergodicregulus/jobtrack/internal/normalise"
	"github.com/ergodicregulus/jobtrack/internal/store"
)

func (a *API) handleGetProfile(w http.ResponseWriter, r *http.Request) error {
	userID := httpx.UserIDFromContext(r.Context())

	profile, err := loadProfile(r.Context(), a.pool, userID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, profile)
	return nil
}

// handlePatchProfile applies a partial profile update.
//
// Writes the profile and its skills in one transaction, then enqueues a
// rescore. The enqueue is inside the transaction on purpose: River stores jobs
// in Postgres, so committing the profile and the "go rescore this user" message
// is a single atomic act. If it were a separate call, a crash between the two
// would leave a user whose profile says one thing and whose match scores
// reflect another, with nothing to detect the drift.
func (a *API) handlePatchProfile(w http.ResponseWriter, r *http.Request) error {
	var update user.ProfileUpdate
	if err := decodeJSON(r, &update); err != nil {
		return err
	}

	userID := httpx.UserIDFromContext(r.Context())
	profile, err := loadProfile(r.Context(), a.pool, userID)
	if err != nil {
		return err
	}
	if err := profile.Apply(update); err != nil {
		return httpx.ErrBadRequest(err.Error())
	}

	if err := a.saveProfile(r.Context(), userID, profile, false); err != nil {
		return err
	}

	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, profile)
	return nil
}

// handleCompleteOnboarding marks onboarding done, refusing if the profile is
// too thin to produce meaningful matches.
//
// Separate from PATCH /v1/me/profile because "save my progress" and "I am
// finished" are different intents with different validation. The wizard saves
// every step through PATCH with no completeness check, so a user can leave and
// come back; only this endpoint asserts the profile is usable.
func (a *API) handleCompleteOnboarding(w http.ResponseWriter, r *http.Request) error {
	var update user.ProfileUpdate
	if err := decodeJSON(r, &update); err != nil {
		return err
	}

	userID := httpx.UserIDFromContext(r.Context())
	profile, err := loadProfile(r.Context(), a.pool, userID)
	if err != nil {
		return err
	}
	if err := profile.Apply(update); err != nil {
		return httpx.ErrBadRequest(err.Error())
	}

	if ok, missing := profile.CanCompleteOnboarding(); !ok {
		return incompleteProfileError(missing)
	}

	if err := a.saveProfile(r.Context(), userID, profile, true); err != nil {
		return err
	}
	profile.Onboarded = true

	httpx.WriteJSON(r.Context(), w, a.log, http.StatusOK, profile)
	return nil
}

// saveProfile persists the profile, its skills, and the rescore request
// atomically.
func (a *API) saveProfile(ctx context.Context, userID int64, p user.Profile, complete bool) error {
	// No rescore is enqueued. Scores are computed on read (ADR-0016), so a
	// profile change takes effect on the next request rather than after a
	// sweep — which also removes the failure mode ADR-0011 documents, where a
	// forgotten version bump left stale scores serving silently.
	err := store.SaveProfile(ctx, a.pool, userID, p, complete, nil)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	return nil
}

// incompleteProfileError names each missing field rather than returning one
// vague message. A form that can highlight the specific field the user skipped
// is the difference between a fixable error and a dead end.
func incompleteProfileError(missing []string) error {
	fields := make([]httpx.FieldError, 0, len(missing))
	for _, f := range missing {
		fields = append(fields, httpx.FieldError{
			Field: f, Code: "required",
			Message: "This is needed before we can match you to roles",
		})
	}
	return httpx.ErrBadRequest("profile is incomplete", fields...)
}

// loadProfile reads a profile and translates the store's absence sentinel into
// the HTTP shape. The store must not know about status codes; this is where
// that translation belongs.
func loadProfile(ctx context.Context, pool *pgxpool.Pool, userID int64) (user.Profile, error) {
	p, err := store.LoadProfile(ctx, pool, userID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return p, httpx.ErrNotFound()
	case err != nil:
		return p, httpx.ErrInternal(err)
	}
	return p, nil
}

// commonSkillSuggestions is the starter list offered in the skill picker.
//
// Served rather than hardcoded in the client, for two reasons that both bit:
// the list existed verbatim in two Svelte files and had already drifted, and it
// held canonical names, so the chips read "postgresql", "node.js" and "aws".
// One source, with the labels the rest of the product uses.
//
// Ordered by how commonly the corpus actually asks for them, not alphabetically
// — a suggestion list is a ranking, and the first six are what most people
// need.
var commonSkillSuggestions = []string{
	"go", "python", "javascript", "typescript", "java", "rust",
	"react", "node.js", "postgresql", "kubernetes", "docker", "aws",
	"terraform", "kafka", "redis", "sql", "linux", "git",
}

// SkillSuggestion is one offered skill.
type SkillSuggestion struct {
	Canonical string `json:"canonical"`
	Label     string `json:"label"`
}

func (a *API) handleCommonSkills(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	out := make([]SkillSuggestion, 0, len(commonSkillSuggestions))
	for _, c := range commonSkillSuggestions {
		out = append(out, SkillSuggestion{Canonical: c, Label: normalise.DisplayName(c)})
	}

	// Public and identical for everyone; it changes when the vocabulary does.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, out)
	return nil
}
