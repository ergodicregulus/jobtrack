package source

import (
	"testing"
	"time"
)

// These moved here from the greenhouse package along with the function itself.
// Three adapters had their own copy of ParseRetryAfter and only one had a test,
// which is how SmartRecruiters' copy came to differ — it dropped the TrimSpace
// and used `> 0` rather than `>= 0` — without anything failing.
func TestParseRetryAfter(t *testing.T) {
	cases := map[string]time.Duration{
		"120":                           2 * time.Minute,
		"":                              0,
		"garbage":                       0,
		"Wed, 21 Oct 2015 07:28:00 GMT": 0, // past date: never a negative wait
		"  30  ":                        30 * time.Second,
		"0":                             0,
		"-5":                            0,
	}
	for in, want := range cases {
		if got := ParseRetryAfter(in); got != want {
			t.Errorf("ParseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty("", "  ", "x", "y"); got != "x" {
		t.Errorf("got %q, want x", got)
	}
	// Whitespace must not win over a real value further down the list: vendors
	// send "" and "   " interchangeably for an absent field.
	if got := FirstNonEmpty("   ", "real"); got != "real" {
		t.Errorf("got %q, want real", got)
	}
	if got := FirstNonEmpty("", " "); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// Moved here from the ashby package with NormalisePeriod. The left column is
// measured from live boards: Ashby 2026-08-19, Recruitee and Workable
// 2026-08-25.
func TestNormalisePeriod(t *testing.T) {
	cases := map[string]string{
		"1 YEAR": "year", "1 MONTH": "month", "1 HOUR": "hour",
		"HOURLY": "hour", "MONTHLY": "month",
		"month": "month", "year": "year", // Recruitee sends these bare.
		"": "", "1 AEON": "",
	}
	for in, want := range cases {
		if got := NormalisePeriod(in); got != want {
			t.Errorf("NormalisePeriod(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormaliseEmployment(t *testing.T) {
	cases := map[string]string{
		"FullTime": "full_time", "Full-time": "full_time", // Ashby, Workable
		"fulltime_permanent": "full_time", "permanent": "full_time", // Recruitee, Personio
		"working-student": "intern", "internship": "intern",
		"freelance": "contract", "contractor": "contract",
		"": "", "wizard": "",
	}
	for in, want := range cases {
		if got := NormaliseEmployment(in); got != want {
			t.Errorf("NormaliseEmployment(%q) = %q, want %q", in, got, want)
		}
	}
}
