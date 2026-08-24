// Package migrations embeds the schema migration files into the binary.
//
// Embedding rather than reading from disk means the migrate binary is
// self-contained: the container image cannot drift from the SQL it is supposed
// to apply, and there is no volume mount to get wrong in production.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
