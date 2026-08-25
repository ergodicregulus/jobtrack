package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jobtrack/jobtrack/internal/auth"
	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/store"
)

// The DPDP rights path.
//
// Real endpoints, not an email address in a policy document. India's DPDP Rules
// were notified 14 November 2025; the Board can levy penalties from 13 November
// 2026. Access, portability, correction and erasure are the four rights, and
// three of them are here — correction is the existing PATCH /v1/me/profile,
// which is the same operation whatever the regulation calls it.

func (a *API) handleListConsents(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	items, err := store.ListConsents(ctx, a.pool, httpx.UserIDFromContext(ctx))
	if err != nil {
		return httpx.ErrInternal(err)
	}
	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, map[string]any{
		"notice_version": store.NoticeVersion,
		"items":          items,
	})
	return nil
}

func (a *API) handleWithdrawConsent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	purpose := r.URL.Query().Get("purpose")
	switch purpose {
	case "resume_parsing", "matching", "digest_email":
	case "account":
		// Withdrawing consent to have an account IS deletion, and pretending
		// otherwise would leave the account running with its consent revoked —
		// a state with no honest meaning.
		return httpx.ErrBadRequest(
			"withdrawing account consent means deleting the account; use DELETE /v1/me/erasure",
			httpx.FieldError{Field: "purpose", Code: "use_erasure", Message: "see /v1/me/erasure"})
	default:
		return httpx.ErrBadRequest("unknown consent purpose",
			httpx.FieldError{Field: "purpose", Code: "invalid",
				Message: "resume_parsing, matching or digest_email"})
	}

	if err := store.WithdrawConsent(ctx, a.pool, httpx.UserIDFromContext(ctx), purpose); err != nil {
		return httpx.ErrInternal(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleExport returns everything held about the caller.
//
// Streamed as a download with a dated filename, because the point of
// portability is that the file leaves and is opened somewhere else.
func (a *API) handleExport(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	data, err := store.ExportUserData(ctx, a.pool, userID)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// The store holds no key by design, so decryption happens here. Exporting
	// ciphertext would satisfy the letter of portability and none of its point.
	if resumes, ok := data["resumes"].([]store.ExportedResume); ok {
		for i := range resumes {
			if len(resumes[i].TextEnc) == 0 {
				continue
			}
			plain, err := a.crypt.Open(resumes[i].TextEnc)
			if err != nil {
				// One unreadable CV must not fail the whole export. Say so in
				// the file rather than omitting it silently, so the reader can
				// tell "we hold nothing" from "we could not read it".
				resumes[i].Text = "[could not be decrypted]"
				continue
			}
			resumes[i].Text = string(plain)
		}
		data["resumes"] = resumes
	}

	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="jobtrack-export-%s.json"`,
			time.Now().UTC().Format("2006-01-02")))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		a.log.ErrorContext(ctx, "export encode failed", "error", err)
	}
	return nil
}

func (a *API) handleRequestErasure(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	at, err := store.RequestErasure(ctx, a.pool, httpx.UserIDFromContext(ctx))
	if err != nil {
		return httpx.ErrInternal(err)
	}

	// The session is already gone — RequestErasure revokes every one of them —
	// so clear the cookie too rather than leaving the browser holding a
	// reference to something that no longer exists.
	http.SetCookie(w, auth.ClearCookie(a.cfg.Security.CookieSecure))

	httpx.WriteJSON(ctx, w, a.log, http.StatusAccepted, map[string]any{
		"requested_at":    at,
		"completes_after": at.Add(store.ErasureGrace),
	})
	return nil
}

func (a *API) handleCancelErasure(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	if err := store.CancelErasure(ctx, a.pool, httpx.UserIDFromContext(ctx)); err != nil {
		return httpx.ErrInternal(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
