// Package auth implements password hashing and session management.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, at the OWASP baseline: m = 19 MiB, t = 2, p = 1.
//
// This lands near 100 ms on a modern server core, which is the right trade
// between user experience and attacker cost. Memory cost is what defeats GPU
// and ASIC attackers far more than time cost does, so if these are ever raised,
// raise memory before iterations.
//
// The parameters are encoded into every hash, so raising them later does not
// invalidate existing passwords — NeedsRehash detects stale hashes and the
// login path upgrades them transparently.
const (
	argonMemoryKiB  uint32 = 19 * 1024
	argonIterations uint32 = 2
	argonSaltLen    uint32 = 16
	argonKeyLen     uint32 = 32
)

var (
	ErrInvalidHash        = errors.New("auth: hash is not in the expected format")
	ErrIncompatibleParams = errors.New("auth: hash was produced by an incompatible argon2 version")
	ErrMismatch           = errors.New("auth: password does not match")
)

// argonParallelism is capped at 4: beyond that the memory cost per lane falls
// and the hash gets cheaper to attack, not harder.
func argonParallelism() uint8 {
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return uint8(n)
}

// HashPassword returns a PHC-format Argon2id hash.
//
// The output is self-describing — it carries the algorithm, version, parameters
// and salt — which is what makes parameter upgrades possible without a
// migration or a second column.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("auth: password must not be empty")
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	p := argonParallelism()
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, p, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonIterations, p,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

type hashParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	key         []byte
}

// VerifyPassword reports whether password matches encoded.
//
// Comparison is constant time. A byte-wise early return would leak how much of
// the hash matched, which is enough to recover it.
func VerifyPassword(password, encoded string) error {
	p, err := decodeHash(encoded)
	if err != nil {
		return err
	}

	other := argon2.IDKey([]byte(password), p.salt,
		p.iterations, p.memory, p.parallelism, uint32(len(p.key)))

	if subtle.ConstantTimeCompare(p.key, other) != 1 {
		return ErrMismatch
	}
	return nil
}

// NeedsRehash reports whether a stored hash used weaker parameters than the
// current policy. The login handler calls this after a successful verify and
// re-hashes in place, so raising parameters upgrades the whole user base
// silently as people sign in.
func NeedsRehash(encoded string) bool {
	p, err := decodeHash(encoded)
	if err != nil {
		return true // unparseable means replace it
	}
	return p.memory < argonMemoryKiB || p.iterations < argonIterations
}

func decodeHash(encoded string) (*hashParams, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return nil, ErrIncompatibleParams
	}

	p := &hashParams{}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d",
		&p.memory, &p.iterations, &p.parallelism); err != nil {
		return nil, ErrInvalidHash
	}

	var err error
	if p.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return nil, ErrInvalidHash
	}
	if p.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return nil, ErrInvalidHash
	}
	if len(p.salt) == 0 || len(p.key) == 0 {
		return nil, ErrInvalidHash
	}
	return p, nil
}

// dummyHash is generated once at startup from a random secret, using the
// current parameters. Generating it rather than hardcoding one guarantees it
// stays valid and correctly-costed if the parameters are ever raised.
var dummyHash = func() string {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic("auth: crypto/rand failed at init: " + err.Error())
	}
	h, err := HashPassword(base64.RawStdEncoding.EncodeToString(secret))
	if err != nil {
		panic("auth: failed to build dummy hash: " + err.Error())
	}
	return h
}()

// DummyVerify performs a real hash comparison against a value nobody knows.
//
// Called when an email does not exist, so login costs the same whether or not
// the account is real. Without it, response latency is a user-enumeration
// oracle — the classic mistake in this exact code path, and one that a
// "return early if user not found" fast path introduces every time.
func DummyVerify(password string) {
	_ = VerifyPassword(password, dummyHash)
}
