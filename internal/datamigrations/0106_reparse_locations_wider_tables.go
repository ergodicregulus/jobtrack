package datamigrations

// ReparseLocationsWiderTables re-runs 0104 after the parser learned the formats
// the rebuilt corpus missed.
//
// Sampled 2026-10-05 over the live postings with no country: ISO-prefixed vendor
// strings ("US-CA-Menlo Park", "GB-London"), a country wrapped in its
// arrangement ("United States (Remote)", "Remote: United States"), spelled-out
// states ("Mountain View, California") and frequent cities the table did not
// know. 0104 has run, and only rows that still fail are revisited, so the same
// backfill under a new version is the whole change.
type ReparseLocationsWiderTables struct{ ReparseLocations }

func (m *ReparseLocationsWiderTables) Version() int64 { return 106 }
func (m *ReparseLocationsWiderTables) Name() string   { return "reparse_locations_wider_tables" }
