// Package mail sends the only outbound email this product has.
//
// net/smtp, not a provider SDK. ADR-0019 records why: the dependency count
// stays at zero, and changing provider becomes a config change rather than a
// rewrite. Anything that speaks SMTP works, including something the operator
// already runs.
//
// What is given up is provider-specific delivery telemetry — bounces, opens,
// complaints — which we have no way to act on yet and no place to put.
package mail

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"mime/quotedprintable"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Sender delivers one message. An interface so the digest job can be tested
// without a mail server, which is the only reason it is not a bare function.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Message is one email. Both bodies are required.
//
// A text part is not a courtesy: some clients render it by preference, and a
// text part that omits the unsubscribe link makes the message non-compliant in
// exactly the clients least able to show an HTML one.
type Message struct {
	To          string
	Subject     string
	Text        string
	HTML        string
	Unsubscribe string // absolute URL, also sent as List-Unsubscribe
}

type SMTPSender struct {
	Host, Username, Password, From string
	Port                           int
}

// Send writes one multipart/alternative message.
//
// Built by hand rather than with a library: the message has two parts and four
// headers, and a MIME library would be a dependency carrying a thousand
// features to save thirty lines.
func (s *SMTPSender) Send(ctx context.Context, m Message) error {
	if s.Host == "" || s.From == "" {
		return fmt.Errorf("mail: sender is not configured")
	}
	if m.To == "" || m.Subject == "" {
		return fmt.Errorf("mail: message needs a recipient and a subject")
	}

	boundary := "jt-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var b strings.Builder

	fmt.Fprintf(&b, "From: %s\r\n", s.From)
	fmt.Fprintf(&b, "To: %s\r\n", m.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	if m.Unsubscribe != "" {
		// Both headers. List-Unsubscribe is what a mail client's own
		// "unsubscribe" button reads, and it works without the reader ever
		// opening the message — which is the version most people use.
		fmt.Fprintf(&b, "List-Unsubscribe: <%s>\r\n", m.Unsubscribe)
		b.WriteString("List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n")
	}
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

	writePart(&b, boundary, "text/plain; charset=utf-8", m.Text)
	writePart(&b, boundary, "text/html; charset=utf-8", m.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)

	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}

	// smtp.SendMail does not take a context, so cancellation is checked before
	// dialling rather than during. A digest run that is being shut down stops
	// starting new sends; one already in flight completes, which is the right
	// way round — a half-sent message is worse than a late one.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := smtp.SendMail(addr, auth, s.From, []string{m.To}, []byte(b.String())); err != nil {
		return fmt.Errorf("mail: send to %s: %w", m.To, err)
	}
	return nil
}

// writePart emits one MIME part, quoted-printable encoded.
//
// Encoding matters here: job titles carry accents, em dashes and CJK, and a raw
// 8-bit body is rejected or mangled by servers that do not advertise 8BITMIME.
func writePart(b *strings.Builder, boundary, contentType, body string) {
	fmt.Fprintf(b, "--%s\r\n", boundary)
	fmt.Fprintf(b, "Content-Type: %s\r\n", contentType)
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")

	var enc strings.Builder
	w := quotedprintable.NewWriter(&enc)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
	b.WriteString(enc.String())
	b.WriteString("\r\n")
}

// UnsubscribeToken signs a user id so an unsubscribe link works without a login.
//
// Without a signature the link is a guessable integer and anyone could
// unsubscribe anyone. HMAC, not encryption: the id is not a secret, the point is
// that we can tell we issued it.
//
// No expiry on purpose. An unsubscribe link in a two-year-old email must still
// work — a dead one leaves the reader with no way out but a spam report.
func UnsubscribeToken(secret string, userID int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "unsubscribe:%d", userID)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyUnsubscribe reports whether a token was issued for this user.
func VerifyUnsubscribe(secret string, userID int64, token string) bool {
	if secret == "" || token == "" {
		return false
	}
	want := UnsubscribeToken(secret, userID)
	// Constant time: a byte-by-byte comparison leaks how much of a forged token
	// was right, which is enough to construct one.
	return hmac.Equal([]byte(want), []byte(token))
}
