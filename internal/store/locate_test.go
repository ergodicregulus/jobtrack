package store

import (
	"testing"

	"github.com/ergodicregulus/jobtrack/internal/source"
)

func TestLocate_FallsBackToTheOfficeOnlyWhenTheLocationNamesNoCountry(t *testing.T) {
	cases := []struct {
		location, office, wantCountry string
	}{
		// Cloudflare: the location is the arrangement, the office is the place.
		{"Hybrid", "Austin, TX, United States", "US"},
		// The employer's own location always wins over the office.
		{"London, United Kingdom", "Austin, TX, United States", "GB"},
		// An office that names no country adds nothing.
		{"Hybrid", "AMER", ""},
		{"Remote", "", ""},
	}
	for _, tc := range cases {
		got := locate(source.RawPosting{LocationRaw: tc.location, Office: tc.office})
		if got.Country != tc.wantCountry {
			t.Errorf("locate(%q, office %q).Country = %q, want %q",
				tc.location, tc.office, got.Country, tc.wantCountry)
		}
		if got.Raw != tc.location {
			t.Errorf("Raw = %q; the location users see must stay %q", got.Raw, tc.location)
		}
	}
}
