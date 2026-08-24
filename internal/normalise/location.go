package normalise

import (
	"regexp"
	"strings"
)

// Location is a parsed place.
type Location struct {
	Country string // ISO-3166 alpha-2
	Region  string // state / province
	City    string
	Raw     string
}

// cityAliases resolves the names that genuinely differ in the wild. Kept
// curated rather than derived: a geocoding service would be a network
// dependency on the ingest hot path, and would still get "Bengaluru" wrong
// often enough to matter.
var cityAliases = map[string]string{
	"bangalore": "Bengaluru", "bengaluru": "Bengaluru", "blr": "Bengaluru",
	"bombay": "Mumbai", "mumbai": "Mumbai",
	"madras": "Chennai", "chennai": "Chennai",
	"calcutta": "Kolkata", "kolkata": "Kolkata",
	"gurgaon": "Gurugram", "gurugram": "Gurugram",
	"new delhi": "Delhi", "delhi": "Delhi", "ncr": "Delhi",
	"hyderabad": "Hyderabad", "pune": "Pune", "noida": "Noida",
	"ahmedabad": "Ahmedabad", "kochi": "Kochi", "cochin": "Kochi",
	"coimbatore": "Coimbatore", "jaipur": "Jaipur", "trivandrum": "Thiruvananthapuram",

	"sf": "San Francisco", "san francisco": "San Francisco",
	"nyc": "New York", "new york": "New York", "new york city": "New York",
	"sea": "Seattle", "seattle": "Seattle", "austin": "Austin",
	"london": "London", "berlin": "Berlin", "amsterdam": "Amsterdam",
	"singapore": "Singapore", "dublin": "Dublin", "toronto": "Toronto",
}

// cityCountry maps a canonical city to its country, so "Bengaluru" alone
// resolves to IN without the posting having to say so.
var cityCountry = map[string]string{
	"Bengaluru": "IN", "Mumbai": "IN", "Chennai": "IN", "Kolkata": "IN",
	"Gurugram": "IN", "Delhi": "IN", "Hyderabad": "IN", "Pune": "IN",
	"Noida": "IN", "Ahmedabad": "IN", "Kochi": "IN", "Coimbatore": "IN",
	"Jaipur": "IN", "Thiruvananthapuram": "IN",

	"San Francisco": "US", "New York": "US", "Seattle": "US", "Austin": "US",
	"London": "GB", "Berlin": "DE", "Amsterdam": "NL",
	"Singapore": "SG", "Dublin": "IE", "Toronto": "CA",
}

var countryNames = map[string]string{
	"india": "IN", "in": "IN",
	"united states": "US", "usa": "US", "us": "US", "u.s.": "US", "america": "US",
	"united kingdom": "GB", "uk": "GB", "england": "GB", "britain": "GB",
	"germany": "DE", "netherlands": "NL", "singapore": "SG",
	"ireland": "IE", "canada": "CA", "australia": "AU",
}

// indiaRegions covers the states our primary market actually posts from.
var indiaRegions = map[string]string{
	"Bengaluru": "KA", "Mumbai": "MH", "Pune": "MH", "Hyderabad": "TG",
	"Chennai": "TN", "Coimbatore": "TN", "Kolkata": "WB", "Delhi": "DL",
	"Gurugram": "HR", "Noida": "UP", "Ahmedabad": "GJ", "Jaipur": "RJ",
	"Kochi": "KL", "Thiruvananthapuram": "KL",
}

// separators splits multi-location strings like
// "Bangalore / Hyderabad / Remote" or "London; Berlin".
var separators = regexp.MustCompile(`\s*[/;|]\s*|\s+or\s+`)

// noise is stripped before parsing: these words appear inside location strings
// but describe the arrangement, not the place.
var locationNoise = regexp.MustCompile(`(?i)\b(remote|hybrid|on-?site|in-?office|flexible|anywhere|multiple locations|various)\b`)

// ParseLocation extracts structured geography from a free-text location.
//
// Multi-location postings ("Bangalore / Hyderabad / Remote") resolve to the
// FIRST concrete place. Storing one is a deliberate simplification: the raw
// string is always shown to the user, so nothing is hidden, and a posting is
// never excluded from a filter because we picked a different city than they
// searched for — the country still matches.
func ParseLocation(raw string) Location {
	loc := Location{Raw: strings.TrimSpace(raw)}
	if loc.Raw == "" {
		return loc
	}

	for _, part := range separators.Split(loc.Raw, -1) {
		cleaned := strings.TrimSpace(locationNoise.ReplaceAllString(part, " "))
		cleaned = strings.Trim(cleaned, " ,-–—")
		if cleaned == "" {
			continue
		}
		if parsed := parseSingleLocation(cleaned); parsed.City != "" || parsed.Country != "" {
			parsed.Raw = loc.Raw
			return parsed
		}
	}

	// Nothing concrete — e.g. "Remote". Country stays empty, which filters read
	// as unknown rather than as a mismatch.
	return loc
}

func parseSingleLocation(s string) Location {
	var out Location

	// Comma-separated is the common shape: "Bengaluru, India" / "Austin, TX, US".
	segments := strings.Split(s, ",")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := strings.ToLower(strings.TrimSpace(segments[i]))
		if seg == "" {
			continue
		}
		if code, ok := countryNames[seg]; ok && out.Country == "" {
			out.Country = code
			continue
		}
		if canonical, ok := cityAliases[seg]; ok && out.City == "" {
			out.City = canonical
			continue
		}
		// A bare two-letter uppercase segment is a US state or country code.
		if out.Region == "" && len(seg) == 2 && i > 0 {
			out.Region = strings.ToUpper(seg)
		}
	}

	if out.City != "" {
		if c, ok := cityCountry[out.City]; ok && out.Country == "" {
			out.Country = c
		}
		if r, ok := indiaRegions[out.City]; ok && (out.Region == "" || out.Country == "IN") {
			out.Region = r
		}
	}
	return out
}
