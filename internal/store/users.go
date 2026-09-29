package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ergodicregulus/jobtrack/internal/domain/user"
)

// LoadProfile reads the queryable half of a user record, plus their skills.
func LoadProfile(ctx context.Context, pool *pgxpool.Pool, userID int64) (user.Profile, error) {
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

	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("load profile: %w", err)
	}

	p.Onboarded = onboardedAt != nil
	p.SkillLabels, err = SkillLabels(ctx, pool, append(append([]string{}, p.Skills...), p.ResumeSkills...))
	if err != nil {
		return p, err
	}
	return p, nil
}

// SkillLabels maps canonical skill names to their display names.
//
// Read from the skills table rather than recomputed, so the profile and the job
// cards can never write the same technology two different ways.
func SkillLabels(ctx context.Context, pool *pgxpool.Pool, canonicals []string) (map[string]string, error) {
	labels := map[string]string{}
	if len(canonicals) == 0 {
		return labels, nil
	}

	rows, err := pool.Query(ctx,
		`SELECT canonical, display_name FROM skills WHERE canonical = ANY($1)`, canonicals)
	if err != nil {
		return nil, fmt.Errorf("skill labels: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var canonical, display string
		if err := rows.Scan(&canonical, &display); err != nil {
			return nil, fmt.Errorf("skill labels: %w", err)
		}
		labels[canonical] = display
	}
	return labels, rows.Err()
}

// SaveProfile writes the profile and its user-declared skills in one transaction.
//
// enqueue runs inside that transaction, and is how the caller schedules a
// rescore. ADR-0005 requires a job to be enqueued in the same transaction as
// the write it depends on: a rescore committed without the profile, or a
// profile committed without its rescore, both leave the user looking at stale
// numbers with nothing to correct them.
func SaveProfile(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID int64,
	p user.Profile,
	complete bool,
	enqueue func(pgx.Tx) error,
) error {
	return InTx(ctx, pool, func(tx pgx.Tx) error {
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
			return fmt.Errorf("update profile: %w", err)
		}

		if err := replaceUserSkills(ctx, tx, userID, p.Skills); err != nil {
			return err
		}
		if enqueue != nil {
			return enqueue(tx)
		}
		return nil
	})
}

// replaceUserSkills swaps the user-declared skill set.
//
// Skills inferred from a resume carry origin='resume' and are left alone: a
// user editing their profile is not implicitly retracting what their CV says.
func replaceUserSkills(ctx context.Context, tx pgx.Tx, userID int64, skills []string) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM user_skills WHERE user_id = $1 AND origin = 'user'`, userID); err != nil {
		return fmt.Errorf("clear user skills: %w", err)
	}
	if len(skills) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO user_skills (user_id, skill_id, origin)
		SELECT $1, s.id, 'user' FROM skills s WHERE s.canonical = ANY($2)
		ON CONFLICT (user_id, skill_id) DO NOTHING`, userID, skills)
	if err != nil {
		return fmt.Errorf("insert user skills: %w", err)
	}
	return nil
}

// LoadPreferences reads the JSONB preferences blob.
func LoadPreferences(ctx context.Context, pool *pgxpool.Pool, userID int64) (user.Preferences, error) {
	var prefs user.Preferences
	err := pool.QueryRow(ctx,
		`SELECT preferences FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&prefs)

	if errors.Is(err, pgx.ErrNoRows) {
		return prefs, ErrNotFound
	}
	if err != nil {
		return prefs, fmt.Errorf("load preferences: %w", err)
	}
	return prefs, nil
}

// SavePreferences replaces the preferences blob wholesale.
func SavePreferences(ctx context.Context, pool *pgxpool.Pool, userID int64, prefs user.Preferences) error {
	_, err := pool.Exec(ctx,
		`UPDATE users SET preferences = $2, updated_at = now() WHERE id = $1`, userID, prefs)
	if err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}
	return nil
}
