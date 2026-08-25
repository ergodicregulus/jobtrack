package crypt

import (
	"bytes"
	"errors"
	"testing"
)

const key = "a-test-key-that-is-at-least-32-chars-long"

func TestRoundTrip(t *testing.T) {
	c, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	want := "Priya Raman\npriya@example.com\nGo, PostgreSQL"
	sealed, err := c.SealString(want)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("priya@example.com")) {
		t.Fatal("plaintext is visible in the ciphertext")
	}
	// Open, not a String wrapper: production decrypts with Open, and a test
	// that exercises a convenience the product never calls is testing itself.
	plain, err := c.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(plain); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A nonce reused across two seals under the same key breaks GCM completely.
// Prepending a fresh random nonce per call is what prevents it, so identical
// plaintext must produce different bytes.
func TestSameInputSealsDifferently(t *testing.T) {
	c, _ := New(key)
	a, _ := c.SealString("same")
	b, _ := c.SealString("same")
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext are byte-identical; the nonce is not random")
	}
}

// Authentication is the point of GCM. A flipped bit must fail loudly rather
// than decrypt to garbage that then gets written into a profile.
func TestTamperedCiphertextIsRejected(t *testing.T) {
	c, _ := New(key)
	sealed, _ := c.SealString("Go, PostgreSQL, Kubernetes")
	sealed[len(sealed)-1] ^= 0x01

	if _, err := c.Open(sealed); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestWrongKeyIsRejected(t *testing.T) {
	a, _ := New(key)
	b, _ := New("a-completely-different-key-also-32-chars")
	sealed, _ := a.SealString("secret")
	if _, err := b.Open(sealed); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

// A short key is an operator mistake, and failing at startup is the only place
// it can be caught before real data depends on it.
func TestShortKeyIsRefused(t *testing.T) {
	if _, err := New("too-short"); err == nil {
		t.Fatal("a short key was accepted")
	}
}

// nil in, nil out — so a NULL column round-trips as NULL rather than becoming
// an empty encrypted blob that reads back as "".
func TestNilRoundTripsAsNil(t *testing.T) {
	c, _ := New(key)
	sealed, err := c.Seal(nil)
	if err != nil || sealed != nil {
		t.Fatalf("Seal(nil) = %v, %v; want nil, nil", sealed, err)
	}
	out, err := c.Open(nil)
	if err != nil || out != nil {
		t.Fatalf("Open(nil) = %v, %v; want nil, nil", out, err)
	}
}
