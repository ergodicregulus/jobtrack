package api

import (
	"net/http"
	"strconv"

	"github.com/ergodicregulus/jobtrack/internal/mail"
	"github.com/ergodicregulus/jobtrack/internal/store"
)

// handleUnsubscribe honours an unsubscribe link without a login.
//
// NO SESSION REQUIRED, and that is the point. Someone who wants out is often
// reading on a phone, signed out, months later — asking them to remember a
// password first is how an unsubscribe link becomes a spam report. The signed
// token is what makes that safe: without it the link would be a guessable
// integer and anyone could unsubscribe anyone.
//
// It writes a WITHDRAWAL to user_consents rather than setting a flag somewhere
// new, so the DPDP record stays the single account of what this person agreed
// to and when they stopped. A second, parallel notion of "unsubscribed" would
// be the thing that eventually disagrees with the consent log.
//
// GET, deliberately, even though it changes state. Mail clients follow links
// with GET and List-Unsubscribe-Post sends an empty POST; refusing GET here
// means the button in the client does nothing. The token is the authorisation,
// and the action is idempotent — unsubscribing twice is unsubscribed.
func (a *API) handleUnsubscribe(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	userID, err := strconv.ParseInt(q.Get("u"), 10, 64)
	secret := a.cfg.Email.UnsubscribeSecret

	// One response for every failure, and no detail in it. Distinguishing "bad
	// token" from "no such user" would turn this into an oracle for which
	// account ids exist.
	if err != nil || !mail.VerifyUnsubscribe(secret, userID, q.Get("t")) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(unsubPage("That link is not valid.",
			"It may have been altered in transit. You can turn digests off in your settings.")))
		return nil
	}

	if err := store.WithdrawConsent(r.Context(), a.pool, userID, "digest_email"); err != nil {
		return nil // already withdrawn is the same outcome the reader asked for
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(unsubPage("You are unsubscribed.",
		"No more digest emails. Your account and saved searches are untouched.")))
	return nil
}

// unsubPage is a whole HTML document because this is the one page a reader may
// reach from an email client with no session, no styles loaded and no
// JavaScript. It has to be complete on its own.
func unsubPage(title, body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + title + `</title></head>` +
		`<body style="font:16px/1.6 -apple-system,Segoe UI,sans-serif;max-width:34rem;` +
		`margin:12vh auto;padding:0 1.5rem;color:#1c1b19">` +
		`<h1 style="font-size:1.4rem">` + title + `</h1><p>` + body + `</p>` +
		`<p><a href="/jobs">Back to JobTrack</a></p></body></html>`
}
