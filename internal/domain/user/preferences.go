// Package user holds user-facing domain types.
package user

import (
	"encoding/json"
	"fmt"
)

// Theme is the user's colour-scheme choice.
//
// Three states, not two. "system" is a real choice distinct from either
// explicit value: it means "follow the OS", and it must survive a round trip so
// a user who never picked a theme is not silently pinned to whatever their OS
// said on the day they signed up.
type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

func (t Theme) Valid() bool {
	switch t {
	case ThemeSystem, ThemeLight, ThemeDark:
		return true
	}
	return false
}

// There is deliberately no Density preference.
//
// One existed, and its stated justification — "a genuine accessibility
// preference for anyone with a motor or vision impairment" — was false. It
// changed row padding and nothing else: not type size, not target size. Someone
// who needs larger text is better served by browser zoom, which scales
// everything rather than the gaps between things.
//
// Rows written before this change still carry a `density` key in their JSONB.
// It is preserved untouched by the unknown-key handling below rather than
// migrated away, because an inert key costs nothing and a migration that
// rewrites every user row to delete one string does not earn its risk.

// FeedSort is a default ordering for the feed.
//
// The values are the feed's own, not a parallel vocabulary: a preference that
// can hold a sort the feed rejects is a preference that silently does nothing.
type FeedSort string

const (
	SortNewest FeedSort = "newest"
	SortMatch  FeedSort = "match"
	SortComp   FeedSort = "comp"
)

func (s FeedSort) Valid() bool {
	switch s {
	case SortNewest, SortMatch, SortComp:
		return true
	}
	return false
}

// PerPageOptions are the results-per-page values the UI offers.
//
// A set rather than a range, even though the feed accepts anything from 1 to
// 50. This is the default behind a select control, and storing 37 would mean
// rendering a control with no matching option — the preference would be real
// and invisible. 50 is the feed's own ceiling and is not duplicated upward.
var PerPageOptions = []int{10, 25, 50}

// Preferences is the non-queryable UI settings blob.
//
// Stored as jsonb. Unknown keys are preserved rather than dropped: during a
// rolling deploy an older release must not silently discard a setting written
// by a newer one.
type Preferences struct {
	Theme Theme `json:"theme"`

	// ReducedMotion mirrors the OS setting when the user has explicitly
	// overridden it. nil means "follow the OS", which is the correct default:
	// prefers-reduced-motion is already honoured in CSS.
	ReducedMotion *bool `json:"reduced_motion,omitempty"`

	// PerPage and Sort are what the feed uses when the URL says nothing.
	//
	// They live here rather than in a column because nothing ever filters or
	// joins on them: they are read once with the user row and written into a
	// query string. A column would buy an index nobody queries.
	//
	// They are deliberately NOT the same thing as a default saved search. A
	// saved search restores a whole set of filters on request; these two are
	// the shape of the list itself, and apply to every view including a search
	// the user has just typed.
	PerPage int      `json:"per_page,omitempty"`
	Sort    FeedSort `json:"sort,omitempty"`

	// unknown carries keys this release does not recognise, so they survive a
	// read-modify-write cycle.
	unknown map[string]json.RawMessage
}

// Defaults returns the preferences a brand-new user has.
func Defaults() Preferences {
	return Preferences{
		Theme:   ThemeSystem,
		PerPage: 25,
		Sort:    SortNewest,
	}
}

// UnmarshalJSON is tolerant by design: an invalid stored value falls back to the
// default rather than failing the request. A corrupt theme string must not stop
// someone signing in.
func (p *Preferences) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*p = Defaults()
	p.unknown = make(map[string]json.RawMessage, len(raw))

	for key, val := range raw {
		switch key {
		case "theme":
			var t Theme
			if json.Unmarshal(val, &t) == nil && t.Valid() {
				p.Theme = t
			}
		case "reduced_motion":
			var b bool
			if json.Unmarshal(val, &b) == nil {
				p.ReducedMotion = &b
			}
		case "per_page":
			var n int
			if json.Unmarshal(val, &n) == nil && validPerPage(n) {
				p.PerPage = n
			}
		case "sort":
			var v FeedSort
			if json.Unmarshal(val, &v) == nil && v.Valid() {
				p.Sort = v
			}
		default:
			// Written by a newer release. Keep it.
			p.unknown[key] = val
		}
	}
	return nil
}

// MarshalJSON writes known fields and re-emits anything it did not recognise.
func (p Preferences) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(p.unknown)+3)
	for k, v := range p.unknown {
		out[k] = v
	}
	out["theme"] = p.Theme
	out["per_page"] = p.PerPage
	out["sort"] = p.Sort
	if p.ReducedMotion != nil {
		out["reduced_motion"] = *p.ReducedMotion
	}
	return json.Marshal(out)
}

// Update applies a partial change, validating each supplied field.
//
// Partial rather than replace: a client that only knows about `theme` must not
// wipe `density` by omitting it.
type Update struct {
	Theme         *Theme    `json:"theme"`
	ReducedMotion *bool     `json:"reduced_motion"`
	PerPage       *int      `json:"per_page"`
	Sort          *FeedSort `json:"sort"`
}

// Apply validates and merges an update. Returns the field name on failure so the
// API can report which one was wrong.
func (p *Preferences) Apply(u Update) error {
	if u.Theme != nil {
		if !u.Theme.Valid() {
			return fmt.Errorf("theme must be one of system, light, dark (got %q)", *u.Theme)
		}
		p.Theme = *u.Theme
	}
	if u.ReducedMotion != nil {
		p.ReducedMotion = u.ReducedMotion
	}
	if u.PerPage != nil {
		if !validPerPage(*u.PerPage) {
			return fmt.Errorf("per_page must be one of %v (got %d)", PerPageOptions, *u.PerPage)
		}
		p.PerPage = *u.PerPage
	}
	if u.Sort != nil {
		if !u.Sort.Valid() {
			return fmt.Errorf("sort must be one of newest, match, comp (got %q)", *u.Sort)
		}
		p.Sort = *u.Sort
	}
	return nil
}

func validPerPage(n int) bool {
	for _, v := range PerPageOptions {
		if v == n {
			return true
		}
	}
	return false
}
