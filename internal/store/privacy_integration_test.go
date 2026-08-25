//go:build integration

package store

import (
	"context"
	"testing"
	"time"
)

// TestRetentionSweepErasesPastGrace covers the DPDP obligation that a stated
// retention period is actually enforced, and it exists because the first
// version of the sweep silently did nothing.
//
// It set pref_countries and pref_modes to NULL. Both are NOT NULL with a '{}'
// default, so the statement aborted — and the job had already reported success
// on an earlier run where there was nothing to erase, which is exactly the
// shape of a compliance control that looks healthy and enforces nothing.
func TestRetentionSweepErasesPastGrace(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	keep := seedUser(t, pool, "keep@example.test")
	erase := seedUser(t, pool, "erase@example.test")

	// Give both a name, so "was it cleared" is a real question.
	if _, err := pool.Exec(ctx,
		`UPDATE users SET first_name = 'Priya', pref_countries = '{IN}' WHERE id = ANY($1)`,
		[]int64{keep, erase}); err != nil {
		t.Fatalf("seed names: %v", err)
	}

	// One requested deletion inside the grace window, one past it.
	if _, err := pool.Exec(ctx,
		`UPDATE users SET deletion_requested_at = now() - interval '1 day' WHERE id = $1`, keep); err != nil {
		t.Fatalf("seed recent request: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE users SET deletion_requested_at = now() - interval '30 days' WHERE id = $1`, erase); err != nil {
		t.Fatalf("seed old request: %v", err)
	}

	res, err := RunRetentionSweep(ctx, pool)
	if err != nil {
		t.Fatalf("RunRetentionSweep: %v", err)
	}
	if res.AccountsErased != 1 {
		t.Fatalf("erased %d accounts, want exactly 1 — the one past its grace window",
			res.AccountsErased)
	}

	var email, firstName *string
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT email, first_name, deleted_at FROM users WHERE id = $1`, erase).
		Scan(&email, &firstName, &deletedAt); err != nil {
		t.Fatalf("read erased user: %v", err)
	}
	if deletedAt == nil {
		t.Error("erased account has no deleted_at")
	}
	if firstName != nil {
		t.Errorf("first_name survived erasure: %q", *firstName)
	}
	if email == nil || *email == "erase@example.test" {
		t.Error("the address survived erasure; it is the identifier that matters most")
	}

	// The account still inside its window must be untouched. A sweep that
	// erases eagerly is worse than one that never runs.
	var keepDeleted *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT deleted_at FROM users WHERE id = $1`, keep).Scan(&keepDeleted); err != nil {
		t.Fatalf("read kept user: %v", err)
	}
	if keepDeleted != nil {
		t.Error("an account one day into a seven-day grace window was erased")
	}
}

// TestRetentionSweepClearsExpiredResumeText covers the other half: CV text past
// its stated period is removed from accounts that are otherwise fine.
func TestRetentionSweepClearsExpiredResumeText(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	userID := seedUser(t, pool, "cv@example.test")

	var expired, current int64
	for _, tc := range []struct {
		label  string
		retain string
		into   *int64
	}{
		{"old", "now() - interval '1 day'", &expired},
		{"fresh", "now() + interval '12 months'", &current},
	} {
		if err := pool.QueryRow(ctx, `
			INSERT INTO resumes (user_id, label, is_default, blob_key, mime_type, byte_size,
			                     parsed_text_enc, parsed_json_enc, parse_confidence,
			                     parser_version, retain_until)
			VALUES ($1, $2, false, '', 'text/plain', 10,
			        '\xdeadbeef'::bytea, '\xdeadbeef'::bytea, 1.0, 'test', `+tc.retain+`)
			RETURNING id`, userID, tc.label).Scan(tc.into); err != nil {
			t.Fatalf("seed %s resume: %v", tc.label, err)
		}
	}

	res, err := RunRetentionSweep(ctx, pool)
	if err != nil {
		t.Fatalf("RunRetentionSweep: %v", err)
	}
	if res.ResumesDeleted != 1 {
		t.Fatalf("cleared %d resumes, want exactly 1", res.ResumesDeleted)
	}

	var expiredLen, currentLen int
	if err := pool.QueryRow(ctx,
		`SELECT octet_length(parsed_text_enc) FROM resumes WHERE id = $1`, expired).
		Scan(&expiredLen); err != nil {
		t.Fatalf("read expired: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT octet_length(parsed_text_enc) FROM resumes WHERE id = $1`, current).
		Scan(&currentLen); err != nil {
		t.Fatalf("read current: %v", err)
	}
	if expiredLen != 0 {
		t.Errorf("expired CV text survived: %d bytes", expiredLen)
	}
	if currentLen == 0 {
		t.Error("a CV inside its retention window was cleared")
	}

	// Idempotent: a second sweep must not re-count rows it already cleared, or
	// the log line stops meaning anything.
	again, err := RunRetentionSweep(ctx, pool)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if again.ResumesDeleted != 0 {
		t.Errorf("second sweep cleared %d again; the sweep is not idempotent",
			again.ResumesDeleted)
	}
}
