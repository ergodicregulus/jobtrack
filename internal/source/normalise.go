package source

import "strings"

// NormaliseEmployment maps a vendor's employment-type vocabulary onto ours.
//
// Every vendor spells these differently and several spell them in more than one
// way: Ashby sends "FullTime", Recruitee "fulltime_permanent", Workable
// "Full-time", Personio "permanent". Unrecognised values return "" rather than a
// guess — an employment type we invented is worse than one we admit we lack,
// because it is filterable and therefore trusted.
func NormaliseEmployment(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	for _, sep := range []string{" ", "-", "_"} {
		s = strings.ReplaceAll(s, sep, "")
	}
	switch s {
	case "fulltime", "fulltimepermanent", "permanent", "regular":
		return "full_time"
	case "parttime", "parttimepermanent":
		return "part_time"
	case "contract", "contractor", "freelance", "fixedterm", "temporarycontract":
		return "contract"
	case "intern", "internship", "trainee", "workingstudent", "apprenticeship":
		return "intern"
	case "temporary", "temp", "seasonal":
		return "temporary"
	default:
		return ""
	}
}

// NormalisePeriod maps a compensation interval onto ours.
//
// The period is never inferred from the magnitude of the figures. A 3,000
// EUR/month role and a 3,000 EUR/year one are both plausible somewhere, and
// guessing turns a correct low salary into a fabricated high one — see the
// Ashby bug where an unbound interval field defaulted every rate to yearly and
// put "$30 – $45 per year" in front of users for an hourly contract.
func NormalisePeriod(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "1 HOUR", "HOUR", "HOURLY", "PER_HOUR":
		return "hour"
	case "1 DAY", "DAY", "DAILY", "PER_DAY":
		return "day"
	case "1 WEEK", "WEEK", "WEEKLY", "PER_WEEK":
		return "week"
	case "1 MONTH", "MONTH", "MONTHLY", "PER_MONTH":
		return "month"
	case "1 YEAR", "YEAR", "YEARLY", "ANNUAL", "ANNUALLY", "PER_YEAR":
		return "year"
	default:
		return ""
	}
}
