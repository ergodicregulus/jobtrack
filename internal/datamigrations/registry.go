// Package datamigrations registers background backfills.
//
// Adding one is deliberately a code change rather than a SQL file: a backfill
// needs batching, a resume cursor and idempotency, and none of those survive
// being expressed as a single statement.
//
// The rule that decides which kind of migration you want:
//
//	Can it finish in seconds on the largest table it touches?
//	  yes -> schema migration (migrations/*.sql), blocks the deploy
//	  no  -> data migration (here), runs in the background
//
// Running a multi-minute backfill as a schema migration is the most common way
// to turn a routine deploy into an outage.
package datamigrations

import (
	"github.com/jobtrack/jobtrack/internal/migrate"
)

// All returns every registered data migration, in version order.
//
// Versions share the number space with schema migrations so the ordering
// between the two is unambiguous when reading `migrate status`.
func All() []migrate.DataMigration {
	return []migrate.DataMigration{
		&BackfillYoEConfidence{},
		&BackfillSmartRecruitersURLs{},
		&CloseLongAbsentPostings{},
	}
}
