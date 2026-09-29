package jobs

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/ergodicregulus/jobtrack/internal/mail"
	"github.com/ergodicregulus/jobtrack/internal/store"
)

// SendDigestsArgs runs one pass over the saved searches that are due.
type SendDigestsArgs struct {
	// Interval is how long since a search's last digest before it is due again.
	Interval time.Duration `json:"interval"`
}

func (SendDigestsArgs) Kind() string { return "send_digests" }

// SendDigestsWorker emails what has arrived since a reader last looked.
//
// On the maintenance queue, never a request path. Idempotent by
// last_digest_at + the interval, so a retry after a partial run resumes rather
// than resending — a duplicate digest is worse than a late one, because it is
// the thing that makes people unsubscribe.
type SendDigestsWorker struct {
	river.WorkerDefaults[SendDigestsArgs]
	Deps *Deps
}

// itemsPerDigest is how many roles the email names.
//
// A digest is a prompt to come back, not a replacement for the feed. Naming a
// hundred is a list nobody reads and a message that trips spam heuristics.
const itemsPerDigest = 8

func (w *SendDigestsWorker) Work(ctx context.Context, job *river.Job[SendDigestsArgs]) error {
	cfg := w.Deps.Cfg.Email
	if !cfg.Enabled {
		// Not an error. A deployment that has not configured email sends
		// nothing, and saying so once per run beats failing per message.
		w.Deps.Log.InfoContext(ctx, "digests skipped: email is disabled")
		return nil
	}
	if cfg.From == "" || cfg.Host == "" || cfg.BaseURL == "" || cfg.UnsubscribeSecret == "" {
		return fmt.Errorf("digest: email is enabled but not configured " +
			"(need EMAIL_SMTP_HOST, EMAIL_FROM, EMAIL_BASE_URL, EMAIL_UNSUBSCRIBE_SECRET)")
	}

	interval := job.Args.Interval
	if interval <= 0 {
		interval = 7 * 24 * time.Hour
	}

	candidates, err := store.DigestCandidates(ctx, w.Deps.Pool, time.Now().Add(-interval), 500)
	if err != nil {
		return err
	}

	sender := &mail.SMTPSender{
		Host: cfg.Host, Port: cfg.Port,
		Username: cfg.Username, Password: cfg.Password, From: cfg.From,
	}

	var sent int
	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}
		items, err := store.DigestItems(ctx, w.Deps.Pool, c.Since, itemsPerDigest)
		if err != nil || len(items) == 0 {
			continue
		}

		unsub := fmt.Sprintf("%s/unsubscribe?u=%d&t=%s",
			strings.TrimRight(cfg.BaseURL, "/"), c.UserID,
			mail.UnsubscribeToken(cfg.UnsubscribeSecret, c.UserID))

		msg := mail.Message{
			To:          c.Email,
			Subject:     digestSubject(c),
			Text:        digestText(c, items, unsub),
			HTML:        digestHTML(c, items, unsub),
			Unsubscribe: unsub,
		}
		if err := sender.Send(ctx, msg); err != nil {
			// One bad address must not stop the run. The next pass retries it,
			// and a permanently bad one simply never succeeds — which is
			// visible in the log rather than as a stalled queue.
			w.Deps.Log.WarnContext(ctx, "digest send failed", "search_id", c.SearchID, "error", err)
			continue
		}
		// AFTER the send. A crash between the two resends a digest, which is a
		// nuisance; the other order drops one silently, which is a promise
		// broken with no trace.
		if err := store.MarkDigestSent(ctx, w.Deps.Pool, c.SearchID); err != nil {
			w.Deps.Log.WarnContext(ctx, "digest mark failed", "search_id", c.SearchID, "error", err)
		}
		sent++
	}

	w.Deps.Log.InfoContext(ctx, "digests sent", "sent", sent, "candidates", len(candidates))
	return nil
}

func digestSubject(c store.DigestCandidate) string {
	n := c.NewPostings
	if n == 1 {
		return fmt.Sprintf("1 new role for %q", c.SearchName)
	}
	return fmt.Sprintf("%d new roles for %q", n, c.SearchName)
}

// One source for both bodies.
//
// They are written together because they have to say the same thing: a text
// part that drops the unsubscribe link makes the message non-compliant in
// exactly the clients least able to render the HTML one.
func digestText(c store.DigestCandidate, items []store.DigestItem, unsub string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d new since you last looked at %q.\r\n\r\n", c.NewPostings, c.SearchName)
	for _, it := range items {
		fmt.Fprintf(&b, "* %s — %s", it.Title, it.Company)
		if it.Location != "" {
			fmt.Fprintf(&b, " (%s)", it.Location)
		}
		fmt.Fprintf(&b, "\r\n  %s\r\n", it.URL)
	}
	if c.NewPostings > len(items) {
		fmt.Fprintf(&b, "\r\n…and %d more.\r\n", c.NewPostings-len(items))
	}
	fmt.Fprintf(&b, "\r\nUnsubscribe: %s\r\n", unsub)
	return b.String()
}

func digestHTML(c store.DigestCandidate, items []store.DigestItem, unsub string) string {
	var b strings.Builder
	b.WriteString(`<div style="font:15px/1.5 -apple-system,Segoe UI,sans-serif;color:#1c1b19">`)
	fmt.Fprintf(&b, "<p>%d new since you last looked at <strong>%s</strong>.</p><ul>",
		c.NewPostings, html.EscapeString(c.SearchName))
	for _, it := range items {
		fmt.Fprintf(&b, `<li style="margin:0 0 10px"><a href="%s">%s</a> — %s`,
			html.EscapeString(it.URL), html.EscapeString(it.Title), html.EscapeString(it.Company))
		if it.Location != "" {
			fmt.Fprintf(&b, ` <span style="color:#6d6a65">(%s)</span>`, html.EscapeString(it.Location))
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
	if c.NewPostings > len(items) {
		fmt.Fprintf(&b, "<p>…and %d more.</p>", c.NewPostings-len(items))
	}
	fmt.Fprintf(&b, `<p style="color:#6d6a65;font-size:13px"><a href="%s">Unsubscribe</a></p></div>`,
		html.EscapeString(unsub))
	return b.String()
}
