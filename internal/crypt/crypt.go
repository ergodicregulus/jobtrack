// Package crypt encrypts the few columns that hold genuinely sensitive text.
//
// Right now that is resume text and the structured parse of it. A CV carries a
// name, contact details, an employment history and often an address — under
// India's DPDP Act 2023 and the GDPR it is personal data with real obligations,
// and it is the most sensitive object this system holds.
//
// Stdlib AES-256-GCM. Not a library: the entire job is "seal this byte slice
// with a key from config", the standard library does exactly that, and a
// dependency here would add a supply chain to a security boundary in exchange
// for nothing.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// ErrCorrupt means the ciphertext failed authentication.
//
// GCM cannot tell "wrong key" from "tampered bytes" and should not try — both
// mean the same thing operationally: do not use this data.
var ErrCorrupt = errors.New("crypt: ciphertext failed authentication")

// Cipher seals and opens values for one key.
type Cipher struct {
	aead cipher.AEAD
}

// New derives a cipher from a configured key string.
//
// SHA-256 of the passphrase rather than requiring exactly 32 raw bytes: the key
// arrives as an environment variable, which is text, and demanding a precise
// byte length there produces either base64 plumbing everywhere or an operator
// silently truncating. Config already enforces a 32-character minimum, so the
// input has real entropy behind it.
func New(key string) (*Cipher, error) {
	if len(key) < 32 {
		return nil, errors.New("crypt: key must be at least 32 characters")
	}
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("crypt: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypt: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts plaintext, returning nonce||ciphertext.
//
// The nonce is prepended rather than stored in its own column: it is not a
// secret, it must never be reused, and keeping it adjacent to the bytes it
// belongs to makes reuse-by-mismatched-row impossible by construction.
func (c *Cipher) Seal(plaintext []byte) ([]byte, error) {
	if plaintext == nil {
		return nil, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypt: nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// SealString is Seal for text, which is what every current caller has.
func (c *Cipher) SealString(s string) ([]byte, error) { return c.Seal([]byte(s)) }

// Open decrypts a value produced by Seal.
func (c *Cipher) Open(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	n := c.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrCorrupt
	}
	out, err := c.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, ErrCorrupt
	}
	return out, nil
}

// OpenString is Open for text.
