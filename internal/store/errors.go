package store

import "errors"

// Sentinels the store returns instead of HTTP errors.
//
// The store must not know it is being called over HTTP — that is the rule
// check_layering.py enforces — so it reports what happened in its own terms and
// the caller decides what status code that is worth.
var (
	// ErrNotFound is returned when a row the caller named does not exist, or
	// exists but belongs to another user. The two are deliberately
	// indistinguishable: telling a caller that a row exists but is not theirs
	// is an enumeration oracle.
	ErrNotFound = errors.New("store: not found")

	// ErrNotRemovable is returned when a row exists but is past the state in
	// which deleting it is safe.
	ErrNotRemovable = errors.New("store: not removable")
)
