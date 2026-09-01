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

// countryNames maps what employers actually write to ISO 3166-1 alpha-2.
//
// It held nine countries and that was the single largest cause of missing
// locations in the corpus. Sampled 2026-09-01 over 150 live postings with no
// country: 71% of them named their country in plain English in the last
// comma-segment — "Shanghai, Shanghai, China", "Budapest, , Hungary", "Tokyo,
// Japan" — and the lookup simply had no entry. SmartRecruiters posts a clean
// City, Region, Country triple every time and 58% of its live postings were
// landing with country NULL because of this map.
//
// The additions below are the countries observed in that sample and in the forty
// most frequent unparsed values, not a world list: a country nobody posts from
// is a line nobody can verify. Add one when a posting needs it.
var countryNames = map[string]string{
	// "in" is deliberately NOT here. It is India's code and also Indiana's, and
	// with it "Springfield, IN" resolved to India. Indian postings spell their
	// city — Bengaluru, Mumbai, Hyderabad — and reach IN through cityCountry, so
	// dropping the bare code loses nothing and stops a US state being read as
	// the primary market.
	"india":         "IN",
	"united states": "US", "usa": "US", "us": "US", "u.s.": "US", "america": "US",
	"united kingdom": "GB", "uk": "GB", "england": "GB", "britain": "GB",
	"germany": "DE", "netherlands": "NL", "singapore": "SG",
	"ireland": "IE", "canada": "CA", "australia": "AU",

	// Observed in the corpus, most frequent first. China alone accounted for 451
	// postings across Shanghai, Suzhou and Wuxi.
	"china": "CN", "japan": "JP", "hungary": "HU", "portugal": "PT",
	"vietnam": "VN", "viet nam": "VN", "thailand": "TH", "france": "FR",
	"spain": "ES", "poland": "PL", "brazil": "BR", "malaysia": "MY",
	"turkey": "TR", "türkiye": "TR", "estonia": "EE", "philippines": "PH",
	"bulgaria": "BG", "serbia": "RS", "slovenia": "SI", "finland": "FI",
	"israel": "IL", "colombia": "CO", "mexico": "MX", "belgium": "BE",
	"italy": "IT", "switzerland": "CH", "austria": "AT", "sweden": "SE",
	"denmark": "DK", "norway": "NO", "czechia": "CZ", "czech republic": "CZ",
	"romania": "RO", "greece": "GR", "south africa": "ZA", "new zealand": "NZ",
	"united arab emirates": "AE", "uae": "AE", "indonesia": "ID",
	"south korea": "KR", "korea": "KR", "taiwan": "TW", "hong kong": "HK",
}

// regionCountry resolves a bare state or province code to its country.
//
// The parser already computed Region and then threw it away: ParseLocation only
// accepted a result carrying a City or a Country, so "Washington, DC" and
// "Vancouver, BC" produced a Region and were discarded. A state code implies its
// country as surely as the word does.
//
// ONLY CODES WITH NO ISO 3166-1 COUNTRY COLLISION APPEAR HERE, and the omissions
// are the point. CA is California and Canada. DE is Delaware and Germany. IN is
// Indiana and India. "Munich, DE" would resolve to Delaware, and a US state is
// not a plausible thing to be wrong about on a country filter someone is
// relying on — a wrong country is worse than an absent one.
//
// The colliding codes are therefore left to the city and country tables, which
// have the context to disambiguate. This map only closes the cases where two
// letters can mean exactly one thing.
var regionCountry = map[string]string{
	// Canadian provinces. NL (Netherlands), ON and PE (Peru) are omitted.
	"AB": "CA", "BC": "CA", "MB": "CA", "NB": "CA", "NS": "CA",
	"NT": "CA", "NU": "CA", "QC": "CA", "SK": "CA", "YT": "CA",

	// US states. Omitted for collision: AL AR AZ CA CO DE GA ID IN KY LA MD ME
	// MN MO MS MT NC NE PA PE SC SD SN TN VA VI.
	"AK": "US", "CT": "US", "DC": "US", "FL": "US", "HI": "US",
	"IA": "US", "IL": "US", "KS": "US", "MA": "US", "MI": "US",
	"ND": "US", "NH": "US", "NJ": "US", "NM": "US", "NV": "US",
	"NY": "US", "OH": "US", "OK": "US", "OR": "US", "RI": "US",
	"TX": "US", "UT": "US", "VT": "US", "WA": "US", "WI": "US",
	"WV": "US", "WY": "US",
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
		// Country included: a segment like "Vancouver, BC" resolves through the
		// region table and would otherwise be discarded for having no city the
		// curated list knows.
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

	// A region we recognise names its country. LAST, so a named city always
	// wins: "Bengaluru, DC" is a Bengaluru posting with a stray code, not a
	// Washington one.
	if out.Country == "" && out.Region != "" {
		if c, ok := regionCountry[out.Region]; ok {
			out.Country = c
		}
	}
	return out
}
