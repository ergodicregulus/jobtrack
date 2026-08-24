package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHashPassword_RoundTrip(t *testing.T) {
	const pw = "correct horse battery staple"

	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := VerifyPassword(pw, hash); err != nil {
		t.Fatalf("VerifyPassword with correct password: %v", err)
	}
	if err := VerifyPassword(pw+"x", hash); !errors.Is(err, ErrMismatch) {
		t.Fatalf("VerifyPassword with wrong password: got %v, want ErrMismatch", err)
	}
}

// Two hashes of the same password must differ. If they match, the salt is not
// being applied — which would make the whole user table crackable with a single
// rainbow table.
func TestHashPassword_SaltIsUnique(t *testing.T) {
	const pw = "same password"
	a, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical: the salt is not random")
	}
	// Both must still verify.
	if err := VerifyPassword(pw, a); err != nil {
		t.Errorf("first hash failed to verify: %v", err)
	}
	if err := VerifyPassword(pw, b); err != nil {
		t.Errorf("second hash failed to verify: %v", err)
	}
}

// The stored format must be self-describing, because that is what allows the
// parameters to be raised later without a migration.
func TestHashPassword_PHCFormat(t *testing.T) {
	hash, err := HashPassword("x")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Fatalf("expected 6 PHC segments, got %d: %q", len(parts), hash)
	}
	if parts[1] != "argon2id" {
		t.Errorf("algorithm = %q, want argon2id", parts[1])
	}
	if parts[2] != "v=19" {
		t.Errorf("version = %q, want v=19", parts[2])
	}
	// The OWASP baseline must be what we actually emit, not just what the
	// comment claims.
	if !strings.Contains(parts[3], "m=19456") || !strings.Contains(parts[3], "t=2") {
		t.Errorf("parameters = %q, want m=19456 and t=2 (OWASP baseline)", parts[3])
	}
}

func TestVerifyPassword_RejectsMalformed(t *testing.T) {
	valid, err := HashPassword("x")
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"empty":              "",
		"not PHC":            "plaintext",
		"wrong algorithm":    "$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"too few segments":   "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA",
		"bad params":         "$argon2id$v=19$garbage$c2FsdA$aGFzaA",
		"bad base64 salt":    "$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaA",
		"truncated real one": valid[:len(valid)-10],
	}

	for name, hash := range cases {
		t.Run(name, func(t *testing.T) {
			if err := VerifyPassword("x", hash); err == nil {
				t.Errorf("malformed hash %q was accepted", hash)
			}
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	current, err := HashPassword("x")
	if err != nil {
		t.Fatal(err)
	}
	if NeedsRehash(current) {
		t.Error("a hash produced by the current policy should not need rehashing")
	}

	// A hash from weaker parameters must be flagged so the login path upgrades
	// it. Without this, raising the policy would only protect new accounts.
	weak := "$argon2id$v=19$m=4096,t=1,p=1$c2FsdHNhbHRzYWx0c2E$" +
		"aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g"
	if !NeedsRehash(weak) {
		t.Error("a hash with m=4096,t=1 should need rehashing")
	}

	if !NeedsRehash("not a hash at all") {
		t.Error("an unparseable hash should need rehashing")
	}
}

// DummyVerify must cost roughly the same as a real verify, or login latency
// reveals whether an account exists.
func TestDummyVerify_CostsSameAsRealVerify(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}

	hash, err := HashPassword("real password")
	if err != nil {
		t.Fatal(err)
	}

	measure := func(fn func()) time.Duration {
		// Best-of-3 to reduce scheduler noise; we are checking an order of
		// magnitude, not a precise figure.
		best := time.Hour
		for range 3 {
			start := time.Now()
			fn()
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}

	real := measure(func() { _ = VerifyPassword("wrong", hash) })
	dummy := measure(func() { DummyVerify("wrong") })

	ratio := float64(dummy) / float64(real)
	if ratio < 0.5 || ratio > 2.0 {
		t.Errorf("DummyVerify took %v vs real verify %v (ratio %.2f); "+
			"a large difference makes login a user-enumeration oracle", dummy, real, ratio)
	}
}

func TestHashPassword_RejectsEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Error("empty password was accepted")
	}
}

// Argon2id at the OWASP baseline should land near 100ms. This is a guard
// against someone "optimising" the parameters into uselessness.
func TestHashPassword_IsDeliberatelySlow(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	start := time.Now()
	if _, err := HashPassword("benchmark"); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("hashing took %v — far below the ~100ms target; "+
			"check that the argon2 parameters have not been weakened", elapsed)
	}
}
