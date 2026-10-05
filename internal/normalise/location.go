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

	// The most frequent cities among live postings with no country, 2026-10-05.
	"san francisco bay area": "San Francisco", "bay area": "San Francisco",
	"chicago": "Chicago", "menlo park": "Menlo Park", "foster city": "Foster City",
	"mountain view": "Mountain View", "palo alto": "Palo Alto", "santa clara": "Santa Clara",
	"nashville": "Nashville", "mexico city": "Mexico City", "tokyo": "Tokyo",
	"barcelona": "Barcelona", "são paulo": "São Paulo", "sao paulo": "São Paulo",
	"paris": "Paris", "munich": "Munich", "münchen": "Munich", "madrid": "Madrid",
	"sydney": "Sydney", "warsaw": "Warsaw",
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

	"Chicago": "US", "Menlo Park": "US", "Foster City": "US", "Mountain View": "US",
	"Palo Alto": "US", "Santa Clara": "US", "Nashville": "US",
	"Mexico City": "MX", "Tokyo": "JP", "Barcelona": "ES", "São Paulo": "BR",
}

// namesakeCityCountry holds cities that share their name with a US town —
// Paris TX, Warsaw IN, Munich ND, Madrid IA, Sydney NS. Alone, the famous one is
// meant; beside a region code it may not be, so these resolve only when the
// string carries no region. "Paris, TX" stays unknown rather than becoming France.
var namesakeCityCountry = map[string]string{
	"Paris": "FR", "Munich": "DE", "Madrid": "ES", "Sydney": "AU", "Warsaw": "PL",
}

// stateNames resolves a spelled-out US state or Canadian province. Unlike the
// two-letter codes none of these collide with a country — except Georgia, which
// is left out.
var stateNames = map[string]string{
	"alabama": "US", "alaska": "US", "arizona": "US", "arkansas": "US", "california": "US",
	"colorado": "US", "connecticut": "US", "delaware": "US", "florida": "US", "hawaii": "US",
	"idaho": "US", "illinois": "US", "indiana": "US", "iowa": "US", "kansas": "US",
	"kentucky": "US", "louisiana": "US", "maine": "US", "maryland": "US", "massachusetts": "US",
	"michigan": "US", "minnesota": "US", "mississippi": "US", "missouri": "US", "montana": "US",
	"nebraska": "US", "nevada": "US", "new hampshire": "US", "new jersey": "US",
	"new mexico": "US", "north carolina": "US", "north dakota": "US", "ohio": "US",
	"oklahoma": "US", "oregon": "US", "pennsylvania": "US", "rhode island": "US",
	"south carolina": "US", "south dakota": "US", "tennessee": "US", "texas": "US",
	"utah": "US", "vermont": "US", "virginia": "US", "washington": "US",
	"west virginia": "US", "wisconsin": "US", "wyoming": "US",
	"ontario": "CA", "quebec": "CA", "british columbia": "CA", "alberta": "CA",
	"manitoba": "CA", "nova scotia": "CA", "saskatchewan": "CA",
}

// isoPrefix reads the vendor shape "US-CA-Menlo Park" and "GB-London": an
// ISO country code, an optional region code, then the city. The hyphenated
// prefix is a format, not prose, so its codes are unambiguous — CA after US- is
// California.
var isoPrefix = regexp.MustCompile(`^([A-Z]{2})-(?:([A-Z]{2})-)?(.+)$`)

// isoCodes are the codes the country table can produce, so a prefix outside it
// ("XY-Somewhere") is not trusted.
var isoCodes = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range countryNames {
		m[c] = true
	}
	return m
}()

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
//
// A spaced dash and a colon separate too: "*HQ - San Francisco, CA" and
// "Remote - US: Select locations" put the place in a later part.
var separators = regexp.MustCompile(`\s*[/;|:]\s*|\s+[-–—]\s+|\s+or\s+`)

// brackets are dropped so "United States (Remote)" reads as its country once the
// arrangement word inside them is gone.
var brackets = regexp.MustCompile(`[()\[\]]`)

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
		cleaned := locationNoise.ReplaceAllString(part, " ")
		cleaned = strings.TrimSpace(brackets.ReplaceAllString(cleaned, " "))
		cleaned = strings.Trim(cleaned, " ,-–—*")
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

	if m := isoPrefix.FindStringSubmatch(s); m != nil && isoCodes[m[1]] {
		out.Country, out.Region = m[1], m[2]
		if canonical, ok := cityAliases[strings.ToLower(strings.TrimSpace(m[3]))]; ok {
			out.City = canonical
		}
		return out
	}

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
		if code, ok := stateNames[seg]; ok && out.Country == "" {
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
		if c, ok := namesakeCityCountry[out.City]; ok && out.Country == "" && out.Region == "" {
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
