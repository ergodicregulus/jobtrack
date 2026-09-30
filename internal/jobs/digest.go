package jobs

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/ergodicregulus/jobtrack/internal/mail"
	"github.com/ergodicregulus/jobtrack/internal/matching"
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

	sender := w.Deps.Mailer
	if sender == nil {
		sender = &mail.SMTPSender{
			Host: cfg.Host, Port: cfg.Port,
			Username: cfg.Username, Password: cfg.Password, From: cfg.From,
		}
	}
	scorer := matching.NewScorer(matching.DefaultConfig(), matching.DefaultAdjacency())

	var sent int
	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}
		items, more, err := digestMatches(ctx, w.Deps, scorer, c)
		if err != nil {
			// A search saved before saving validated its query can fail to
			// parse. Skipped and logged, never emailed a guess.
			w.Deps.Log.WarnContext(ctx, "digest skipped", "search_id", c.SearchID, "error", err)
			continue
		}
		if len(items) == 0 {
			continue // "nothing new" is the email people unsubscribe from
		}

		base := strings.TrimRight(cfg.BaseURL, "/")
		unsub := fmt.Sprintf("%s/unsubscribe?u=%d&t=%s", base, c.UserID,
			mail.UnsubscribeToken(cfg.UnsubscribeSecret, c.UserID))
		d := digest{Search: c.SearchName, Items: items, More: more,
			SeeAll: base + "/jobs?" + c.Query, Unsubscribe: unsub}

		msg := mail.Message{
			To: c.Email, Subject: d.subject(), Text: d.text(), HTML: d.html(), Unsubscribe: unsub,
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

// digestMatches replays the saved search, as its owner, over roles posted since
// they last looked.
//
// Through store.Feed, the same code the feed page runs, so the email and the
// page cannot disagree about what matches. The first version did not read the
// search at all: it sent the corpus's newest roles, and a count of every new
// posting, under the subscriber's own search name.
//
// One extra row is fetched and never shown. Its presence is how the email knows
// to say "more than" without claiming a total it did not count.
func digestMatches(
	ctx context.Context, d *Deps, scorer *matching.Scorer, c store.DigestCandidate,
) ([]store.FeedItem, bool, error) {
	values, err := url.ParseQuery(c.Query)
	if err != nil {
		return nil, false, fmt.Errorf("saved query: %w", err)
	}
	f, err := store.FeedFilterFromQuery(values)
	if err != nil {
		return nil, false, fmt.Errorf("saved query: %w", err)
	}
	f.UserID = &c.UserID // their dismissals hidden, their bands scored
	f.PostedWithin = 0   // a relative window would fight the absolute boundary
	f.PostedAfter = c.Since
	f.Sort = "newest"
	f.Limit = itemsPerDigest + 1

	page, err := store.Feed(ctx, d.Pool, scorer, f, "")
	if err != nil {
		return nil, false, err
	}
	items := page.Items
	if len(items) > itemsPerDigest {
		return items[:itemsPerDigest], true, nil
	}
	return items, false, nil
}

// digest is one email's content. Both bodies render from it because they have
// to say the same thing: a text part that drops the unsubscribe link makes the
// message non-compliant in exactly the clients least able to render the HTML.
type digest struct {
	Search      string
	Items       []store.FeedItem
	More        bool // more matched than Items holds
	SeeAll      string
	Unsubscribe string
}

// count is the headline number, and never more than was actually found.
func (d digest) count() string {
	if d.More {
		return fmt.Sprintf("More than %d new roles", len(d.Items))
	}
	if len(d.Items) == 1 {
		return "1 new role"
	}
	return fmt.Sprintf("%d new roles", len(d.Items))
}

func (d digest) subject() string { return fmt.Sprintf("%s for %q", d.count(), d.Search) }

func (d digest) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s matching %q.\r\n\r\n", d.count(), d.Search)
	for _, it := range d.Items {
		fmt.Fprintf(&b, "* %s — %s", it.Title, it.CompanyName)
		if it.LocationRaw != "" {
			fmt.Fprintf(&b, " (%s)", it.LocationRaw)
		}
		fmt.Fprintf(&b, "\r\n  %s\r\n", it.ApplyURL)
	}
	if d.More {
		fmt.Fprintf(&b, "\r\nSee them all: %s\r\n", d.SeeAll)
	}
	fmt.Fprintf(&b, "\r\nUnsubscribe: %s\r\n", d.Unsubscribe)
	return b.String()
}

func (d digest) html() string {
	var b strings.Builder
	b.WriteString(`<div style="font:15px/1.5 -apple-system,Segoe UI,sans-serif;color:#1c1b19">`)
	fmt.Fprintf(&b, "<p>%s matching <strong>%s</strong>.</p><ul>",
		html.EscapeString(d.count()), html.EscapeString(d.Search))
	for _, it := range d.Items {
		fmt.Fprintf(&b, `<li style="margin:0 0 10px"><a href="%s">%s</a> — %s`,
			html.EscapeString(it.ApplyURL), html.EscapeString(it.Title), html.EscapeString(it.CompanyName))
		if it.LocationRaw != "" {
			fmt.Fprintf(&b, ` <span style="color:#6d6a65">(%s)</span>`, html.EscapeString(it.LocationRaw))
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
	if d.More {
		fmt.Fprintf(&b, `<p><a href="%s">See them all</a></p>`, html.EscapeString(d.SeeAll))
	}
	fmt.Fprintf(&b, `<p style="color:#6d6a65;font-size:13px"><a href="%s">Unsubscribe</a></p></div>`,
		html.EscapeString(d.Unsubscribe))
	return b.String()
}
