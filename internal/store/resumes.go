package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewResume is everything needed to record a parsed CV.
//
// The original file is never among them. ADR-0007: we store the extracted text
// encrypted and nothing else, so a breach cannot leak the document itself.
type NewResume struct {
	UserID          int64
	Label           string
	MimeType        string
	ByteSize        int
	TextEnc         []byte
	JSONEnc         []byte
	ParseConfidence float64
	ParserVersion   string
}

// InsertResume records a parsed CV and returns its id and creation time.
//
// is_default is set only when the user has no other CV. Silently re-pointing
// the default at a file someone was merely trying out would change every score
// they see without them asking.
func InsertResume(ctx context.Context, pool *pgxpool.Pool, r NewResume) (int64, time.Time, error) {
	var id int64
	var createdAt time.Time
	err := pool.QueryRow(ctx, `
		INSERT INTO resumes (user_id, label, is_default, blob_key, mime_type, byte_size,
		                     parsed_text_enc, parsed_json_enc, parse_confidence, parser_version)
		VALUES ($1, $2,
		        NOT EXISTS (SELECT 1 FROM resumes WHERE user_id = $1),
		        '', $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`,
		r.UserID, r.Label, r.MimeType, r.ByteSize,
		r.TextEnc, r.JSONEnc, r.ParseConfidence, r.ParserVersion).Scan(&id, &createdAt)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("insert resume: %w", err)
	}
	return id, createdAt, nil
}

// ResumeJSON reads the encrypted parse result for one of a user's resumes.
//
// Decryption is the caller's job: the key belongs to the service, not to the
// data layer, and the store has no business holding it.
func ResumeJSON(ctx context.Context, pool *pgxpool.Pool, userID, resumeID int64) ([]byte, error) {
	var enc []byte
	err := pool.QueryRow(ctx,
		`SELECT parsed_json_enc FROM resumes WHERE id = $1 AND user_id = $2`,
		resumeID, userID).Scan(&enc)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read resume: %w", err)
	}
	return enc, nil
}

// KnownSkill is one canonical skill with its label and whether a user has it.
type KnownSkill struct {
	Canonical string `db:"canonical"`
	Label     string `db:"display_name"`
	Held      bool   `db:"held"`
}

// LookupSkills answers "does this profile already have it" and "how is it
// written" for a set of canonical names.
//
// One query, not one per skill: a CV yields twenty skills, and twenty round
// trips for a boolean is the definition of an N+1.
func LookupSkills(ctx context.Context, pool *pgxpool.Pool, userID int64, canonicals []string) ([]KnownSkill, error) {
	if len(canonicals) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT s.canonical,
		       s.display_name,
		       EXISTS (SELECT 1 FROM user_skills us
		                WHERE us.skill_id = s.id AND us.user_id = $1) AS held
		  FROM skills s WHERE s.canonical = ANY($2)`, userID, canonicals)
	if err != nil {
		return nil, fmt.Errorf("lookup skills: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[KnownSkill])
	if err != nil {
		return nil, fmt.Errorf("lookup skills: %w", err)
	}
	return out, nil
}

// ApplyResume replaces the resume-derived half of a profile.
//
// setYears is applied only when the parse actually produced a number; yoe is
// nil otherwise. enqueue runs inside the transaction for the reason ADR-0005
// gives: a commit that lands without the rescore leaves every score stale
// against a profile that just changed materially.
func ApplyResume(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, resumeID int64,
	skills []string,
	yoe *int,
	enqueue func(pgx.Tx) error,
) error {
	return InTx(ctx, pool, func(tx pgx.Tx) error {
		// Replace this resume's contribution wholesale rather than merging: the
		// user has just told us exactly what they want from it, and leaving
		// behind skills they unchecked would make the review meaningless.
		if _, err := tx.Exec(ctx,
			`DELETE FROM user_skills WHERE user_id = $1 AND origin = 'resume'`, userID); err != nil {
			return fmt.Errorf("clear resume skills: %w", err)
		}
		if len(skills) > 0 {
			// DO NOTHING on conflict, so a skill the user had already declared
			// by hand keeps origin='user'. Their statement outranks our
			// inference.
			if _, err := tx.Exec(ctx, `
				INSERT INTO user_skills (user_id, skill_id, origin)
				SELECT $1, s.id, 'resume' FROM skills s WHERE s.canonical = ANY($2)
				ON CONFLICT (user_id, skill_id) DO NOTHING`, userID, skills); err != nil {
				return fmt.Errorf("insert resume skills: %w", err)
			}
		}
		if yoe != nil {
			if _, err := tx.Exec(ctx,
				`UPDATE users SET total_yoe = $2, updated_at = now() WHERE id = $1`,
				userID, *yoe); err != nil {
				return fmt.Errorf("set years of experience: %w", err)
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE resumes SET is_default = (id = $2), updated_at = now() WHERE user_id = $1`,
			userID, resumeID); err != nil {
			return fmt.Errorf("set default resume: %w", err)
		}
		if enqueue != nil {
			return enqueue(tx)
		}
		return nil
	})
}
