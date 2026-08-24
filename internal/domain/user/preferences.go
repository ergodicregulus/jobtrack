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

	// unknown carries keys this release does not recognise, so they survive a
	// read-modify-write cycle.
	unknown map[string]json.RawMessage
}

// Defaults returns the preferences a brand-new user has.
func Defaults() Preferences {
	return Preferences{
		Theme: ThemeSystem,
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
	Theme         *Theme `json:"theme"`
	ReducedMotion *bool  `json:"reduced_motion"`
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
	return nil
}
