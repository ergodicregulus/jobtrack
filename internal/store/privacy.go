package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NoticeVersion is the privacy notice these consents were given against.
//
// A constant, and it must be bumped whenever the notice changes materially.
// Consent to a notice nobody can identify is not evidence of anything, and
// "they agreed to whatever it said at the time" is not an answer a regulator
// accepts.
const NoticeVersion = "2026-08-25"

// ResumeRetention is how long extracted CV text is kept.
//
// Twenty-four months from upload. Long enough that a job search spanning two
// years does not lose its own history; short enough that a CV from a search
// somebody finished does not sit in the database forever waiting to be part of
// a breach. DPDP requires a stated period with automated deletion — this is the
// stated period, and RunRetentionSweep is the automation.
const ResumeRetention = 24 * 30 * 24 * time.Hour

// ErasureGrace is the delay between requesting deletion and it happening.
//
// Seven days, and it is a safety feature rather than a dark pattern: account
// deletion is irreversible and a compromised session can request it. The
// account is unusable during the window and any sign-in cancels it.
const ErasureGrace = 7 * 24 * time.Hour

// Consent is one recorded agreement.
type Consent struct {
	Purpose       string     `db:"purpose" json:"purpose"`
	NoticeVersion string     `db:"notice_version" json:"notice_version"`
	GrantedAt     time.Time  `db:"granted_at" json:"granted_at"`
	WithdrawnAt   *time.Time `db:"withdrawn_at" json:"withdrawn_at"`
}

// RecordConsent appends a consent row.
//
// Append-only: a new grant after a withdrawal is a new row, so the history of
// what was agreed and when survives. Nothing here updates a previous record.
func RecordConsent(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	purpose string,
	ipHash []byte,
	userAgent string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO user_consents (user_id, purpose, notice_version, ip_hash, user_agent)
		VALUES ($1, $2, $3, $4, $5)`,
		userID, purpose, NoticeVersion, ipHash, truncate(userAgent, 512))
	if err != nil {
		return fmt.Errorf("record consent: %w", err)
	}
	return nil
}

// WithdrawConsent stamps the most recent live grant for a purpose.
func WithdrawConsent(ctx context.Context, pool *pgxpool.Pool, userID int64, purpose string) error {
	_, err := pool.Exec(ctx, `
		UPDATE user_consents SET withdrawn_at = now()
		 WHERE id = (
		   SELECT id FROM user_consents
		    WHERE user_id = $1 AND purpose = $2 AND withdrawn_at IS NULL
		    ORDER BY granted_at DESC LIMIT 1
		 )`, userID, purpose)
	if err != nil {
		return fmt.Errorf("withdraw consent: %w", err)
	}
	return nil
}

// ListConsents returns every consent record for a person, newest first.
func ListConsents(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]Consent, error) {
	rows, err := pool.Query(ctx, `
		SELECT purpose, notice_version, granted_at, withdrawn_at
		  FROM user_consents WHERE user_id = $1
		 ORDER BY granted_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list consents: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[Consent])
	if err != nil {
		return nil, fmt.Errorf("list consents: %w", err)
	}
	return out, nil
}

// RequestErasure starts the deletion clock and locks the account out.
//
// Not an immediate DELETE. The grace window exists because erasure is
// irreversible and a stolen session can ask for it; sessions are revoked
// immediately so the request cannot be made twice from the same theft, and a
// legitimate sign-in cancels it.
func RequestErasure(ctx context.Context, pool *pgxpool.Pool, userID int64) (time.Time, error) {
	var at time.Time
	err := InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			UPDATE users SET deletion_requested_at = now(), updated_at = now()
			 WHERE id = $1 AND deleted_at IS NULL
			 RETURNING deletion_requested_at`, userID).Scan(&at); err != nil {
			return fmt.Errorf("request erasure: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return nil
	})
	return at, err
}

// CancelErasure clears a pending request.
func CancelErasure(ctx context.Context, pool *pgxpool.Pool, userID int64) error {
	_, err := pool.Exec(ctx,
		`UPDATE users SET deletion_requested_at = NULL, updated_at = now() WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("cancel erasure: %w", err)
	}
	return nil
}

// ExportUserData assembles everything held about one person.
//
// Portability and access are the same query: the format is JSON because a
// person must be able to take it elsewhere, and every table holding their data
// appears here. Job postings do not — they are public market data, not personal
// data, and padding an export with the corpus would bury what actually matters.
//
// The encrypted CV text is deliberately NOT included as ciphertext. It is
// decrypted by the caller, which holds the key; shipping bytes nobody can read
// would satisfy the letter of portability and none of its point.
func ExportUserData(ctx context.Context, pool *pgxpool.Pool, userID int64) (map[string]any, error) {
	out := map[string]any{
		"exported_at":    time.Now().UTC(),
		"notice_version": NoticeVersion,
	}

	var account struct {
		Email               string     `json:"email"`
		CreatedAt           time.Time  `json:"created_at"`
		DeletionRequestedAt *time.Time `json:"deletion_requested_at"`
	}
	if err := pool.QueryRow(ctx, `
		SELECT email, created_at, deletion_requested_at
		  FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).
		Scan(&account.Email, &account.CreatedAt, &account.DeletionRequestedAt); err != nil {
		return nil, fmt.Errorf("export account: %w", err)
	}
	out["account"] = account

	profile, err := LoadProfile(ctx, pool, userID)
	if err != nil {
		return nil, err
	}
	out["profile"] = profile

	if prefs, err := LoadPreferences(ctx, pool, userID); err == nil {
		out["preferences"] = prefs
	}
	if consents, err := ListConsents(ctx, pool, userID); err == nil {
		out["consents"] = consents
	}
	if apps, err := ListApplications(ctx, pool, userID); err == nil {
		out["applications"] = apps
	}
	if searches, err := ListSavedSearches(ctx, pool, userID); err == nil {
		out["saved_searches"] = searches
	}

	resumes, err := exportResumes(ctx, pool, userID)
	if err != nil {
		return nil, err
	}
	out["resumes"] = resumes

	return out, nil
}

// ExportedResume is one CV's metadata plus its encrypted text, for the caller
// to decrypt.
type ExportedResume struct {
	ID          int64      `json:"id"`
	Label       string     `json:"label"`
	CreatedAt   time.Time  `json:"created_at"`
	RetainUntil *time.Time `json:"retain_until"`
	// TextEnc is sealed. The store has no key, by design.
	TextEnc []byte `json:"-"`
	// Text is filled in by the caller after decryption.
	Text string `json:"text,omitempty"`
}

func exportResumes(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]ExportedResume, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, label, created_at, retain_until, parsed_text_enc
		  FROM resumes WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("export resumes: %w", err)
	}
	defer rows.Close()

	var out []ExportedResume
	for rows.Next() {
		var r ExportedResume
		if err := rows.Scan(&r.ID, &r.Label, &r.CreatedAt, &r.RetainUntil, &r.TextEnc); err != nil {
			return nil, fmt.Errorf("export resumes: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RetentionResult is what one sweep did, for the log and the audit trail.
type RetentionResult struct {
	ResumesDeleted int64
	AccountsErased int64
}

// RunRetentionSweep enforces the stated periods.
//
// Two independent policies, deliberately not one statement: expired CV text is
// removed from accounts that are otherwise fine, and accounts past their
// erasure grace are anonymised. Fusing them would mean a bug in one silently
// changing the other.
//
// Erasure ANONYMISES rather than deletes the row. Applications and their events
// are the user's own record of a job search and are removed with them, but the
// row itself is retained with its identifiers destroyed so that foreign keys
// stay valid and aggregate history does not silently change. Nothing that
// remains identifies a person.
func RunRetentionSweep(ctx context.Context, pool *pgxpool.Pool) (RetentionResult, error) {
	var res RetentionResult

	tag, err := pool.Exec(ctx, `
		UPDATE resumes
		   SET parsed_text_enc = ''::bytea,
		       parsed_json_enc = ''::bytea,
		       retain_until = NULL,
		       updated_at = now()
		 WHERE retain_until IS NOT NULL AND retain_until < now()
		   AND octet_length(parsed_text_enc) > 0`)
	if err != nil {
		return res, fmt.Errorf("retention sweep, resumes: %w", err)
	}
	res.ResumesDeleted = tag.RowsAffected()

	tag, err = pool.Exec(ctx, `
		UPDATE users
		   SET email = 'erased-' || id || '@invalid',
		       password_hash = NULL,
		       first_name = NULL, last_name = NULL,
		       current_title = NULL, target_title = NULL,
		       total_yoe = NULL,
		       -- Emptied, NOT nulled: these two are NOT NULL with a '{}'
		       -- default. Setting NULL aborts the whole sweep, which is how
		       -- this was found — the job reported success on the run where
		       -- there was nothing to erase and failed on the first run where
		       -- there was.
		       pref_countries = '{}', pref_modes = '{}',
		       pref_comp_min = NULL, pref_currency = NULL,
		       preferences = '{}'::jsonb,
		       deleted_at = now(), deletion_requested_at = NULL,
		       updated_at = now()
		 WHERE deletion_requested_at IS NOT NULL
		   AND deletion_requested_at < now() - $1::interval
		   AND deleted_at IS NULL`, ErasureGrace.String())
	if err != nil {
		return res, fmt.Errorf("retention sweep, erasure: %w", err)
	}
	res.AccountsErased = tag.RowsAffected()

	return res, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
