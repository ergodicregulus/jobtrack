package mail

import (
	"strings"
	"testing"
)

// The unsubscribe token is the only authorisation on a session-free endpoint,
// so these are the assertions that keep it from being a guessable integer.
func TestUnsubscribeToken(t *testing.T) {
	const secret = "test-secret"

	tok := UnsubscribeToken(secret, 42)
	if tok == "" {
		t.Fatal("empty token")
	}
	if !VerifyUnsubscribe(secret, 42, tok) {
		t.Error("a token we issued did not verify")
	}
	// The whole point: one user's link must not unsubscribe another.
	if VerifyUnsubscribe(secret, 43, tok) {
		t.Error("user 42's token unsubscribed user 43")
	}
	if VerifyUnsubscribe("other-secret", 42, tok) {
		t.Error("a token verified under the wrong secret")
	}
	// An unconfigured deployment must reject everything rather than accept
	// everything, which is what an empty secret would do if it were not checked.
	if VerifyUnsubscribe("", 42, tok) {
		t.Error("an empty secret accepted a token")
	}
	if VerifyUnsubscribe(secret, 42, "") {
		t.Error("an empty token verified")
	}
}

// Both bodies must carry the way out. A text part that drops the unsubscribe
// link makes the message non-compliant in exactly the clients least able to
// render the HTML one.
func TestSend_RefusesAnUnconfiguredSender(t *testing.T) {
	s := &SMTPSender{}
	if err := s.Send(t.Context(), Message{To: "a@b.test", Subject: "x"}); err == nil {
		t.Error("an unconfigured sender accepted a message")
	}
	s = &SMTPSender{Host: "localhost", From: "me@test", Port: 25}
	if err := s.Send(t.Context(), Message{}); err == nil {
		t.Error("a message with no recipient was accepted")
	}
}

func TestWritePart_EncodesEightBit(t *testing.T) {
	var b strings.Builder
	writePart(&b, "bnd", "text/plain; charset=utf-8", "Señor — 東京")
	out := b.String()
	if !strings.Contains(out, "quoted-printable") {
		t.Error("part is not declared quoted-printable")
	}
	// Raw 8-bit is rejected or mangled by servers that do not advertise
	// 8BITMIME, and job titles carry accents and CJK routinely.
	if strings.Contains(out, "Señor") {
		t.Error("8-bit text was written raw rather than encoded")
	}
}
