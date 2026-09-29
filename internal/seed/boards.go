// Package seed registers the real ATS boards JobTrack ingests from.
//
// These are genuine, publicly-published board tokens. Every posting ingested
// from them carries the employer's real apply URL, so clicking Apply lands in
// that company's actual requisition queue — which is the entire product thesis
// and the thing synthetic seed data cannot demonstrate.
//
// Discovery is the unglamorous problem nobody writes about: there is no
// directory of Greenhouse or Ashby customers and no way to enumerate them
// through the API, so the list is curated and grows through user submissions.
// See docs/research/source-catalog.md#discovery.
//
// Every token below was verified live on 2026-08-15 by requesting the vendor's
// public endpoint and confirming a non-empty job array. Tokens are NOT guessed
// from company names — that was how an earlier revision shipped a dozen dead
// boards whose postings carried apply URLs that 404ed. A token that cannot be
// verified does not go in this list. See TestBoardsAreWellFormed, and
// `make verify-boards` for the live re-check.
package seed

import "github.com/ergodicregulus/jobtrack/internal/source"

// Board is one company's feed.
type Board struct {
	Slug    string
	Name    string
	Domain  string
	Country string
	Vendor  source.Vendor
	// Token is the vendor's board identifier, taken from the company's public
	// careers URL — e.g. job-boards.greenhouse.io/{token}.
	Token string
	// Openings is the count observed at verification time. Recorded so drift is
	// visible: a board that silently drops to zero has usually migrated ATS
	// vendors, not stopped hiring.
	Openings int
}

// Boards is the starter set: 75 boards, roughly 15,100 live postings.
//
// The mix is deliberate on three axes. Vendors are represented heavily
// enough to exercise the normalisation paths that only differ across vendors —
// Ashby's structured compensation against Greenhouse's text-embedded ranges.
// Four markets (US, EU, UK, IN) appear so location normalisation and currency
// handling are exercised by real data rather than by tests alone. And company
// size spans 6-opening startups to 800-opening platforms, which is what makes
// the adaptive polling tiers observable — a board with six roles genuinely does
// not change often enough to justify a two-hour poll.
func Boards() []Board {
	return []Board{
		// --- Greenhouse: US / global ---
		{Slug: "stripe", Name: "Stripe", Domain: "stripe.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "stripe", Openings: 578},
		{Slug: "databricks", Name: "Databricks", Domain: "databricks.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "databricks", Openings: 809},
		{Slug: "anthropic", Name: "Anthropic", Domain: "anthropic.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "anthropic", Openings: 439},
		{Slug: "mongodb", Name: "MongoDB", Domain: "mongodb.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "mongodb", Openings: 409},
		{Slug: "datadog", Name: "Datadog", Domain: "datadoghq.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "datadog", Openings: 425},
		{Slug: "cloudflare", Name: "Cloudflare", Domain: "cloudflare.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "cloudflare", Openings: 305},
		{Slug: "brex", Name: "Brex", Domain: "brex.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "brex", Openings: 293},
		{Slug: "samsara", Name: "Samsara", Domain: "samsara.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "samsara", Openings: 258},
		{Slug: "elastic", Name: "Elastic", Domain: "elastic.co", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "elastic", Openings: 257},
		{Slug: "pinterest", Name: "Pinterest", Domain: "pinterest.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "pinterest", Openings: 222},
		{Slug: "gitlab", Name: "GitLab", Domain: "gitlab.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "gitlab", Openings: 196},
		{Slug: "affirm", Name: "Affirm", Domain: "affirm.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "affirm", Openings: 194},
		{Slug: "airbnb", Name: "Airbnb", Domain: "airbnb.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "airbnb", Openings: 186},
		{Slug: "lyft", Name: "Lyft", Domain: "lyft.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "lyft", Openings: 171},
		{Slug: "coinbase", Name: "Coinbase", Domain: "coinbase.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "coinbase", Openings: 167},
		{Slug: "figma", Name: "Figma", Domain: "figma.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "figma", Openings: 162},
		{Slug: "reddit", Name: "Reddit", Domain: "reddit.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "reddit", Openings: 151},
		{Slug: "grafana", Name: "Grafana Labs", Domain: "grafana.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "grafanalabs", Openings: 148},
		{Slug: "asana", Name: "Asana", Domain: "asana.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "asana", Openings: 132},
		{Slug: "robinhood", Name: "Robinhood", Domain: "robinhood.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "robinhood", Openings: 124},
		{Slug: "instacart", Name: "Instacart", Domain: "instacart.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "instacart", Openings: 112},
		{Slug: "postman", Name: "Postman", Domain: "postman.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "postman", Openings: 109},
		{Slug: "vercel", Name: "Vercel", Domain: "vercel.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "vercel", Openings: 83},
		{Slug: "discord", Name: "Discord", Domain: "discord.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "discord", Openings: 50},
		{Slug: "dropbox", Name: "Dropbox", Domain: "dropbox.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "dropbox", Openings: 35},

		// --- Greenhouse: Europe / UK ---
		{Slug: "sumup", Name: "SumUp", Domain: "sumup.com", Country: "GB",
			Vendor: source.VendorGreenhouse, Token: "sumup", Openings: 380},
		{Slug: "hellofresh", Name: "HelloFresh", Domain: "hellofresh.com", Country: "DE",
			Vendor: source.VendorGreenhouse, Token: "hellofresh", Openings: 372},
		{Slug: "celonis", Name: "Celonis", Domain: "celonis.com", Country: "DE",
			Vendor: source.VendorGreenhouse, Token: "celonis", Openings: 258},
		{Slug: "doctolib", Name: "Doctolib", Domain: "doctolib.com", Country: "FR",
			Vendor: source.VendorGreenhouse, Token: "doctolib", Openings: 137},
		{Slug: "n26", Name: "N26", Domain: "n26.com", Country: "DE",
			Vendor: source.VendorGreenhouse, Token: "n26", Openings: 79},

		// --- Greenhouse: India ---
		// Thin on purpose, not by preference. Most Indian employers run Lever,
		// Darwinbox, Keka or an in-house portal, and the large product companies
		// that do use Greenhouse often gate the board behind their own careers
		// site rather than publishing the JSON endpoint. This is the honest
		// extent of what is publicly available, and the reason the source
		// catalogue tracks vendor coverage per market.
		{Slug: "phonepe", Name: "PhonePe", Domain: "phonepe.com", Country: "IN",
			Vendor: source.VendorGreenhouse, Token: "phonepe", Openings: 76},
		{Slug: "slice", Name: "slice", Domain: "sliceit.com", Country: "IN",
			Vendor: source.VendorGreenhouse, Token: "slice", Openings: 43},
		{Slug: "razorpay", Name: "Razorpay", Domain: "razorpay.com", Country: "IN",
			Vendor: source.VendorGreenhouse, Token: "razorpaysoftwareprivatelimited", Openings: 24},
		{Slug: "groww", Name: "Groww", Domain: "groww.in", Country: "IN",
			Vendor: source.VendorGreenhouse, Token: "groww", Openings: 8},

		// --- Ashby ---
		// Over-weighted relative to its market share on purpose: Ashby is the
		// only vendor returning STRUCTURED compensation on the list endpoint,
		// and disclosed salary is both a ghost-job integrity signal and one of
		// the most-used filters.
		{Slug: "openai", Name: "OpenAI", Domain: "openai.com", Country: "US",
			Vendor: source.VendorAshby, Token: "openai", Openings: 746},
		{Slug: "harvey", Name: "Harvey", Domain: "harvey.ai", Country: "US",
			Vendor: source.VendorAshby, Token: "harvey", Openings: 393},
		{Slug: "elevenlabs", Name: "ElevenLabs", Domain: "elevenlabs.io", Country: "GB",
			Vendor: source.VendorAshby, Token: "elevenlabs", Openings: 242},
		{Slug: "sierra", Name: "Sierra", Domain: "sierra.ai", Country: "US",
			Vendor: source.VendorAshby, Token: "sierra", Openings: 191},
		{Slug: "cohere", Name: "Cohere", Domain: "cohere.com", Country: "CA",
			Vendor: source.VendorAshby, Token: "cohere", Openings: 144},
		{Slug: "ramp", Name: "Ramp", Domain: "ramp.com", Country: "US",
			Vendor: source.VendorAshby, Token: "ramp", Openings: 136},
		{Slug: "decagon", Name: "Decagon", Domain: "decagon.ai", Country: "US",
			Vendor: source.VendorAshby, Token: "decagon", Openings: 134},
		{Slug: "notion", Name: "Notion", Domain: "notion.so", Country: "US",
			Vendor: source.VendorAshby, Token: "notion", Openings: 133},
		{Slug: "cursor", Name: "Cursor", Domain: "cursor.com", Country: "US",
			Vendor: source.VendorAshby, Token: "cursor", Openings: 114},
		{Slug: "vanta", Name: "Vanta", Domain: "vanta.com", Country: "US",
			Vendor: source.VendorAshby, Token: "vanta", Openings: 96},
		{Slug: "replit", Name: "Replit", Domain: "replit.com", Country: "US",
			Vendor: source.VendorAshby, Token: "replit", Openings: 75},
		{Slug: "ashby", Name: "Ashby", Domain: "ashbyhq.com", Country: "US",
			Vendor: source.VendorAshby, Token: "ashby", Openings: 60},
		{Slug: "docker", Name: "Docker", Domain: "docker.com", Country: "US",
			Vendor: source.VendorAshby, Token: "docker", Openings: 59},
		{Slug: "supabase", Name: "Supabase", Domain: "supabase.com", Country: "US",
			Vendor: source.VendorAshby, Token: "supabase", Openings: 54},
		{Slug: "render", Name: "Render", Domain: "render.com", Country: "US",
			Vendor: source.VendorAshby, Token: "render", Openings: 35},
		{Slug: "linear", Name: "Linear", Domain: "linear.app", Country: "US",
			Vendor: source.VendorAshby, Token: "linear", Openings: 33},
		{Slug: "modal", Name: "Modal", Domain: "modal.com", Country: "US",
			Vendor: source.VendorAshby, Token: "modal", Openings: 30},
		{Slug: "hex", Name: "Hex", Domain: "hex.tech", Country: "US",
			Vendor: source.VendorAshby, Token: "hex", Openings: 30},
		{Slug: "warp", Name: "Warp", Domain: "warp.dev", Country: "US",
			Vendor: source.VendorAshby, Token: "warp", Openings: 16},
		{Slug: "substack", Name: "Substack", Domain: "substack.com", Country: "US",
			Vendor: source.VendorAshby, Token: "substack", Openings: 13},
		{Slug: "zapier", Name: "Zapier", Domain: "zapier.com", Country: "US",
			Vendor: source.VendorAshby, Token: "zapier", Openings: 12},
		{Slug: "posthog", Name: "PostHog", Domain: "posthog.com", Country: "GB",
			Vendor: source.VendorAshby, Token: "posthog", Openings: 11},
		{Slug: "railway", Name: "Railway", Domain: "railway.com", Country: "US",
			Vendor: source.VendorAshby, Token: "railway", Openings: 8},
		{Slug: "browserbase", Name: "Browserbase", Domain: "browserbase.com", Country: "US",
			Vendor: source.VendorAshby, Token: "browserbase", Openings: 8},
		{Slug: "neon", Name: "Neon", Domain: "neon.tech", Country: "US",
			Vendor: source.VendorAshby, Token: "neon", Openings: 6},
		{Slug: "patreon", Name: "Patreon", Domain: "patreon.com", Country: "US",
			Vendor: source.VendorAshby, Token: "patreon", Openings: 6},
		{Slug: "quora", Name: "Quora", Domain: "quora.com", Country: "US",
			Vendor: source.VendorAshby, Token: "quora", Openings: 5},

		// --- SmartRecruiters ---
		//
		// Added for two reasons the roadmap distinguishes between: capability
		// and coverage.
		//
		// CAPABILITY: SmartRecruiters splits a posting into named sections, one
		// of which is "Qualifications". Every other source hands us one blob,
		// which is why 73.7% of extracted skills land as merely `mentioned` —
		// the must/nice split needs an explicit requirements heading and most
		// postings have none. These boards produce real must-haves.
		//
		// COVERAGE: India was the thinnest market at 278 live postings from
		// every source combined. Bosch alone publishes 527 there.
		//
		// Verified live 2026-08-17. Counts are the observed totals, so a board
		// that silently drops to zero is visible as drift rather than as
		// nothing.
		{Slug: "bosch", Name: "Bosch", Domain: "bosch.com", Country: "DE",
			Vendor: source.VendorSmartRecruiters, Token: "BoschGroup", Openings: 4803},
		{Slug: "swiggy", Name: "Swiggy", Domain: "swiggy.com", Country: "IN",
			Vendor: source.VendorSmartRecruiters, Token: "Swiggy", Openings: 46},
		{Slug: "wise", Name: "Wise", Domain: "wise.com", Country: "GB",
			Vendor: source.VendorSmartRecruiters, Token: "Wise", Openings: 438},
		{Slug: "ubisoft", Name: "Ubisoft", Domain: "ubisoft.com", Country: "FR",
			Vendor: source.VendorSmartRecruiters, Token: "Ubisoft2", Openings: 273},
		// --- Recruitee ---
		// The richest list endpoint of any vendor: full description, structured
		// salary WITH a period, and explicit workplace flags, all in one
		// request. Boards are small — Recruitee sells to European SMEs — so
		// these are worth more per posting than per board.
		{Slug: "channable", Name: "Channable", Domain: "channable.com", Country: "NL",
			Vendor: source.VendorRecruitee, Token: "channable", Openings: 15},
		{Slug: "nmbrs", Name: "Nmbrs", Domain: "nmbrs.com", Country: "NL",
			Vendor: source.VendorRecruitee, Token: "nmbrs", Openings: 4},
		{Slug: "hotelchamp", Name: "Hotelchamp", Domain: "hotelchamp.com", Country: "NL",
			Vendor: source.VendorRecruitee, Token: "hotelchamp", Openings: 1},

		// --- Workable ---
		// One request per board including descriptions, via details=true.
		// published_on is a date with no time, so every Workable posting carries
		// PostedAtIsEstimate and none of them can feed the ingest-latency
		// measurement.
		{Slug: "blueground", Name: "Blueground", Domain: "theblueground.com", Country: "GR",
			Vendor: source.VendorWorkable, Token: "blueground", Openings: 26},
		{Slug: "skroutz", Name: "Skroutz", Domain: "skroutz.gr", Country: "GR",
			Vendor: source.VendorWorkable, Token: "skroutz", Openings: 9},
		{Slug: "epignosis", Name: "Epignosis", Domain: "epignosishq.com", Country: "GR",
			Vendor: source.VendorWorkable, Token: "epignosis", Openings: 5},
		{Slug: "persado", Name: "Persado", Domain: "persado.com", Country: "US",
			Vendor: source.VendorWorkable, Token: "persado", Openings: 3},

		// --- Personio ---
		// The only vendor publishing SENIORITY and a years-of-experience range
		// as structured fields, and one of only three with a real time of day on
		// the posting date. German-language descriptions are deliberate: they
		// exercise the normalisation paths that a corpus of English postings
		// never reaches.
		{Slug: "urbansportsclub", Name: "Urban Sports Club", Domain: "urbansportsclub.com", Country: "DE",
			Vendor: source.VendorPersonio, Token: "urbansportsclub", Openings: 38},
		{Slug: "orderbird", Name: "orderbird", Domain: "orderbird.com", Country: "DE",
			Vendor: source.VendorPersonio, Token: "orderbird", Openings: 5},
		{Slug: "personio", Name: "Personio", Domain: "personio.com", Country: "DE",
			Vendor: source.VendorPersonio, Token: "personio", Openings: 1},
		// --- Keka ---
		// India-first, and the reason it was built ahead of the other verified
		// candidate: India was 7.1% of the corpus for a product that names it as
		// the primary market. The token is tenant:portal/orgID — the org
		// identifier is a per-tenant GUID read once from the careers page, and a
		// guessed one returns an empty array rather than a 404, which is
		// indistinguishable from a company that stopped hiring.
		{Slug: "spyneai", Name: "Spyne", Domain: "spyne.ai", Country: "IN",
			Vendor: source.VendorKeka,
			Token:  "spyneai:default/49556835-6902-4481-b4e9-11910edf9cb1", Openings: 16},
		{Slug: "awfis", Name: "Awfis", Domain: "awfis.com", Country: "IN",
			Vendor: source.VendorKeka,
			Token:  "awfis:default/0d7c8703-f6ae-46fc-8058-8e4874ed3070", Openings: 11},
		{Slug: "softprodigy", Name: "SoftProdigy", Domain: "softprodigy.com", Country: "IN",
			Vendor: source.VendorKeka,
			Token:  "softprodigy:default/c6dbd897-0831-47b1-b411-0e5c4bf13354", Openings: 1},
		// --- BambooHR ---
		// Two-phase: the list carries no description, so every posting needs a
		// detail fetch. Boards are small — ten postings is typical — so one poll
		// covers a whole board.
		{Slug: "flyio", Name: "Fly.io", Domain: "fly.io", Country: "US",
			Vendor: source.VendorBambooHR, Token: "flyio", Openings: 10},
		{Slug: "posthog", Name: "PostHog", Domain: "posthog.com", Country: "GB",
			Vendor: source.VendorBambooHR, Token: "posthog", Openings: 1},
		// --- Engineering-dense additions, verified live 2026-09-01 ---
		//
		// Added to dilute a corpus that was 31% one industrial conglomerate and
		// only ~34% engineering-titled. Every token below was called before it
		// was written down, and the Openings figure is what the endpoint
		// returned. Ordered by measured engineering density, highest first.
		//
		// andurilindustries was found, confirmed (2,186 postings) and
		// DELIBERATELY LEFT OUT: 2,000+ roles at ~16% software, mostly
		// mechanical, electrical and manufacturing. Adding it would reproduce
		// exactly the skew these boards exist to correct.
		{Slug: "poolside", Name: "Poolside", Domain: "poolside.ai", Country: "FR",
			Vendor: source.VendorAshby, Token: "poolside", Openings: 15},
		{Slug: "character-ai", Name: "Character.AI", Domain: "character.ai", Country: "US",
			Vendor: source.VendorAshby, Token: "character", Openings: 13},
		{Slug: "togetherai", Name: "Together AI", Domain: "together.ai", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "togetherai", Openings: 58},
		{Slug: "anyscale", Name: "Anyscale", Domain: "anyscale.com", Country: "US",
			Vendor: source.VendorAshby, Token: "anyscale", Openings: 19},
		{Slug: "clickhouse", Name: "ClickHouse", Domain: "clickhouse.com", Country: "US",
			Vendor: source.VendorAshby, Token: "clickhouse", Openings: 173},
		{Slug: "roblox", Name: "Roblox", Domain: "roblox.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "roblox", Openings: 226},
		{Slug: "confluent", Name: "Confluent", Domain: "confluent.io", Country: "US",
			Vendor: source.VendorAshby, Token: "confluent", Openings: 23},
		{Slug: "cockroachlabs", Name: "Cockroach Labs", Domain: "cockroachlabs.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "cockroachlabs", Openings: 26},
		{Slug: "launchdarkly", Name: "LaunchDarkly", Domain: "launchdarkly.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "launchdarkly", Openings: 49},
		{Slug: "workos", Name: "WorkOS", Domain: "workos.com", Country: "US",
			Vendor: source.VendorAshby, Token: "workos", Openings: 28},
		{Slug: "langchain", Name: "LangChain", Domain: "langchain.com", Country: "US",
			Vendor: source.VendorAshby, Token: "langchain", Openings: 104},
		{Slug: "cartesia", Name: "Cartesia", Domain: "cartesia.ai", Country: "US",
			Vendor: source.VendorAshby, Token: "cartesia", Openings: 33},
		{Slug: "scaleai", Name: "Scale AI", Domain: "scale.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "scaleai", Openings: 214},
		{Slug: "snowflake", Name: "Snowflake", Domain: "snowflake.com", Country: "US",
			Vendor: source.VendorAshby, Token: "snowflake", Openings: 384},

		// India presence, which was 7.3% of the corpus. Atlan is India-founded
		// and every one of its postings is India-located; Druva is Pune-founded;
		// Rubrik and Turing both run substantial Bengaluru engineering.
		{Slug: "atlan", Name: "Atlan", Domain: "atlan.com", Country: "IN",
			Vendor: source.VendorAshby, Token: "atlan", Openings: 5},
		{Slug: "druva", Name: "Druva", Domain: "druva.com", Country: "IN",
			Vendor: source.VendorGreenhouse, Token: "druva", Openings: 42},
		{Slug: "rubrik", Name: "Rubrik", Domain: "rubrik.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "rubrik", Openings: 136},
		{Slug: "turing", Name: "Turing", Domain: "turing.com", Country: "US",
			Vendor: source.VendorGreenhouse, Token: "turing", Openings: 25},
	}
}
