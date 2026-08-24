package normalise

import (
	"regexp"
	"strconv"
	"strings"
)

// Compensation is a parsed pay range.
//
// Nil bounds mean NOT DISCLOSED, which must stay distinguishable from zero all
// the way to the UI: only ~80% of postings disclose salary, and collapsing
// "undisclosed" into "below your floor" would silently drop a fifth of the
// market from every filtered search.
type Compensation struct {
	Min      *float64
	Max      *float64
	Currency string
	Period   string // year | month | hour
	Source   string // structured | parsed_text
}

// Disclosed reports whether the employer published a figure.
func (c Compensation) Disclosed() bool { return c.Min != nil || c.Max != nil }

var currencySymbols = map[string]string{
	"₹": "INR", "rs.": "INR", "rs": "INR", "inr": "INR",
	"$": "USD", "usd": "USD", "us$": "USD",
	"€": "EUR", "eur": "EUR",
	"£": "GBP", "gbp": "GBP",
	"s$": "SGD", "sgd": "SGD",
	"c$": "CAD", "cad": "CAD",
	"a$": "AUD", "aud": "AUD",
}

// Patterns are ordered most-specific first. LPA is checked before generic
// number ranges because "28-42 LPA" would otherwise parse as 28 to 42 rupees.
var compPatterns = []struct {
	re         *regexp.Regexp
	multiplier float64
	period     string
	currency   string
}{
	// Indian: "₹28-42 LPA", "28 - 42 LPA", "INR 28L - 42L per annum"
	{regexp.MustCompile(`(?i)(?:₹|rs\.?|inr)?\s*(\d{1,3}(?:\.\d+)?)\s*(?:l|lpa|lakhs?)?\s*[-–—to]+\s*(\d{1,3}(?:\.\d+)?)\s*(?:l|lpa|lakhs?)`), 100_000, "year", "INR"},
	// Single LPA figure: "₹35 LPA"
	{regexp.MustCompile(`(?i)(?:₹|rs\.?|inr)\s*(\d{1,3}(?:\.\d+)?)\s*(?:l|lpa|lakhs?)\b`), 100_000, "year", "INR"},
	// Crore: "₹1.2 - 1.8 Cr"
	{regexp.MustCompile(`(?i)(?:₹|rs\.?|inr)?\s*(\d{1,2}(?:\.\d+)?)\s*(?:cr|crores?)?\s*[-–—to]+\s*(\d{1,2}(?:\.\d+)?)\s*(?:cr|crores?)`), 10_000_000, "year", "INR"},
	// Western k-notation: "$180k – $220k", "£90k-£120k"
	{regexp.MustCompile(`(?i)([$€£])\s*(\d{2,4})\s*k\s*[-–—to]+\s*[$€£]?\s*(\d{2,4})\s*k`), 1_000, "year", ""},
	// Full numbers with separators: "$180,000 - $220,000"
	{regexp.MustCompile(`(?i)([$€£])\s*(\d{1,3}(?:,\d{3})+)\s*[-–—to]+\s*[$€£]?\s*(\d{1,3}(?:,\d{3})+)`), 1, "year", ""},
	// Hourly: "$65 - $85 per hour"
	{regexp.MustCompile(`(?i)([$€£])\s*(\d{2,3})\s*[-–—to]+\s*[$€£]?\s*(\d{2,3})\s*(?:/|per\s+)?(?:hr|hour)`), 1, "hour", ""},
}

// ParseCompensation extracts a pay range from description text.
//
// Used only when the vendor exposes no structured field — Ashby always does,
// Greenhouse only on its detail endpoint. A parsed value is marked
// `parsed_text` so the UI can distinguish a published range from one we read
// out of prose, and so a parsing bug is visible in aggregate rather than
// silently polluting the data.
func ParseCompensation(text string) Compensation {
	// Only scan a window around pay-related words. Scanning the whole
	// description matches version numbers, team sizes and dates — the classic
	// way naive salary parsers produce nonsense.
	window := compensationWindow(text)
	if window == "" {
		return Compensation{}
	}

	for _, p := range compPatterns {
		m := p.re.FindStringSubmatch(window)
		if m == nil {
			continue
		}

		currency := p.currency
		nums := m[1:]
		if currency == "" {
			// The first capture is the symbol for the Western patterns.
			currency = currencySymbols[strings.ToLower(m[1])]
			nums = m[2:]
		}
		if currency == "" {
			currency = inferCurrency(window)
		}

		lo := parseNumber(nums[0]) * p.multiplier
		var hi float64
		if len(nums) > 1 && nums[1] != "" {
			hi = parseNumber(nums[1]) * p.multiplier
		}

		if !plausible(lo, hi, currency, p.period) {
			continue
		}

		c := Compensation{Currency: currency, Period: p.period, Source: "parsed_text"}
		c.Min = &lo
		if hi > 0 {
			c.Max = &hi
		}
		return c
	}
	return Compensation{}
}

var compKeyword = regexp.MustCompile(`(?i)(salary|compensation|pay|ctc|package|remuneration|base|lpa)`)

// compensationWindow narrows to text near a pay keyword.
func compensationWindow(text string) string {
	loc := compKeyword.FindStringIndex(text)
	if loc == nil {
		// Currency symbols alone are enough of a signal to scan a short window.
		if i := strings.IndexAny(text, "₹$€£"); i >= 0 {
			return sliceAround(text, i, 200)
		}
		return ""
	}
	return sliceAround(text, loc[0], 300)
}

func sliceAround(s string, idx, radius int) string {
	start := max(0, idx-radius/3)
	end := min(len(s), idx+radius)
	return s[start:end]
}

func parseNumber(s string) float64 {
	s = strings.ReplaceAll(s, ",", "")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func inferCurrency(text string) string {
	lower := strings.ToLower(text)
	for sym, code := range currencySymbols {
		if strings.Contains(lower, sym) {
			return code
		}
	}
	return ""
}

// plausible rejects parses that are obviously wrong.
//
// Without this a "Java 17" in the requirements becomes a ₹17 salary. Bounds are
// deliberately generous — the goal is to reject nonsense, not to encode a view
// about what a role should pay.
func plausible(lo, hi float64, currency, period string) bool {
	if lo <= 0 {
		return false
	}
	if hi > 0 && hi < lo {
		return false
	}
	// A range spanning more than 10x is almost certainly two unrelated numbers.
	if hi > 0 && hi/lo > 10 {
		return false
	}

	switch {
	case period == "hour":
		return lo >= 5 && lo <= 2_000
	case currency == "INR":
		// ₹1L to ₹20Cr per year.
		return lo >= 100_000 && lo <= 200_000_000
	case currency == "USD" || currency == "EUR" || currency == "GBP":
		return lo >= 10_000 && lo <= 5_000_000
	default:
		return lo >= 1_000
	}
}
