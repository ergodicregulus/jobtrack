package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/domain/user"
	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/jobs"
	"github.com/jobtrack/jobtrack/internal/normalise"
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
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		UPDATE users SET
			first_name     = $2,
			last_name      = $3,
			current_title  = NULLIF($4, ''),
			target_title   = NULLIF($5, ''),
			total_yoe      = $6,
			pref_countries = $7,
			pref_modes     = $8::work_mode[],
			pref_comp_min  = NULLIF($9, 0),
			pref_currency  = NULLIF($10, ''),
			onboarded_at   = CASE WHEN $11 THEN COALESCE(onboarded_at, now()) ELSE onboarded_at END,
			updated_at     = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		userID, p.FirstName, p.LastName, p.CurrentTitle, p.TargetTitle,
		p.TotalYoE, p.Countries, p.Modes, p.CompMin, p.Currency, complete); err != nil {
		return httpx.ErrInternal(err)
	}

	// Replace the user-declared skills wholesale. Skills inferred from a resume
	// carry origin='resume' and are left alone: a user editing their profile is
	// not implicitly retracting what their CV says.
	if _, err := tx.Exec(ctx,
		`DELETE FROM user_skills WHERE user_id = $1 AND origin = 'user'`, userID); err != nil {
		return httpx.ErrInternal(err)
	}
	if len(p.Skills) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_skills (user_id, skill_id, origin)
			SELECT $1, s.id, 'user' FROM skills s WHERE s.canonical = ANY($2)
			ON CONFLICT (user_id, skill_id) DO NOTHING`, userID, p.Skills); err != nil {
			return httpx.ErrInternal(err)
		}
	}

	if _, err := a.river.InsertTx(ctx, tx, jobs.ScoreUserArgs{UserID: userID}, nil); err != nil {
		return httpx.ErrInternal(err)
	}

	if err := tx.Commit(ctx); err != nil {
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

func loadProfile(ctx context.Context, pool *pgxpool.Pool, userID int64) (user.Profile, error) {
	var p user.Profile
	var onboardedAt *string

	err := pool.QueryRow(ctx, `
		SELECT COALESCE(u.first_name, ''),
		       COALESCE(u.last_name, ''),
		       COALESCE(u.current_title, ''),
		       COALESCE(u.target_title, ''),
		       COALESCE(u.total_yoe, 0),
		       (u.total_yoe IS NOT NULL),
		       COALESCE(u.pref_countries, '{}'),
		       COALESCE(u.pref_modes, '{}')::text[],
		       COALESCE(u.pref_comp_min, 0),
		       COALESCE(u.pref_currency, ''),
		       u.onboarded_at::text,
		       COALESCE(ARRAY(
		           SELECT s.canonical FROM user_skills us
		             JOIN skills s ON s.id = us.skill_id
		            WHERE us.user_id = u.id AND us.origin = 'user'
		            ORDER BY s.canonical
		       ), '{}'),
		       COALESCE(ARRAY(
		           SELECT s.canonical FROM user_skills us
		             JOIN skills s ON s.id = us.skill_id
		            WHERE us.user_id = u.id AND us.origin = 'resume'
		            ORDER BY s.canonical
		       ), '{}')
		  FROM users u
		 WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
		Scan(&p.FirstName, &p.LastName, &p.CurrentTitle, &p.TargetTitle,
			&p.TotalYoE, &p.YoEStated, &p.Countries, &p.Modes, &p.CompMin,
			&p.Currency, &onboardedAt, &p.Skills, &p.ResumeSkills)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return p, httpx.ErrNotFound()
	case err != nil:
		return p, httpx.ErrInternal(err)
	}

	p.Onboarded = onboardedAt != nil

	// Labels for exactly the skills this user has. Read from the skills table
	// rather than recomputed, so the profile and the job cards can never write
	// the same technology two different ways.
	p.SkillLabels = map[string]string{}
	all := append(append([]string{}, p.Skills...), p.ResumeSkills...)
	if len(all) > 0 {
		rows, err := pool.Query(ctx,
			`SELECT canonical, display_name FROM skills WHERE canonical = ANY($1)`, all)
		if err != nil {
			return p, httpx.ErrInternal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var canonical, display string
			if err := rows.Scan(&canonical, &display); err != nil {
				return p, httpx.ErrInternal(err)
			}
			p.SkillLabels[canonical] = display
		}
		if err := rows.Err(); err != nil {
			return p, httpx.ErrInternal(err)
		}
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
