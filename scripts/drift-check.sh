#!/usr/bin/env sh
# Does the live schema match what the migrations produce?
#
# Rebuilds the schema from scratch in a throwaway database, introspects both,
# and diffs. A difference means someone changed the live database out of band —
# a hand-run ALTER during an incident, a migration edited after it was applied,
# or a migration that is not idempotent in the way its author assumed.
#
# Deliberately NOT Atlas, which consistency-and-drift.md names. That document
# was written before the migrations existed; Atlas would add a tool to the image
# AND a second declaration of the schema to keep in step — one more pair of
# things that can drift, to detect drift.
#
#   make drift-check
set -eu

: "${DATABASE_URL:?DATABASE_URL is required}"

SCRATCH="jobtrack_drift_check"

# Swap the database name, preserving user, host and query string. sed rather
# than a URL parser because sh has none and the shape is fixed by compose.
swap_db() { echo "$1" | sed -E "s#(://[^/]+)/[^?]+#\1/$2#"; }

ADMIN_URL="$(swap_db "$DATABASE_URL" postgres)"
SCRATCH_URL="$(swap_db "$DATABASE_URL" "$SCRATCH")"

cleanup() {
  psql -X -q -d "$ADMIN_URL" -c "DROP DATABASE IF EXISTS $SCRATCH WITH (FORCE)" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "→ rebuilding the schema from migrations/ in a scratch database"
cleanup
psql -X -q -v ON_ERROR_STOP=1 -d "$ADMIN_URL" -c "CREATE DATABASE $SCRATCH" >/dev/null

# The same binary and the same migration files, against an empty database.
DATABASE_URL="$SCRATCH_URL" go run ./cmd/migrate up >/dev/null

introspect() {
  psql -X -q -v ON_ERROR_STOP=1 -d "$1" -f scripts/drift-check.sql | sed '/^$/d' | sort
}

introspect "$SCRATCH_URL"  > /tmp/drift-expected.txt
introspect "$DATABASE_URL" > /tmp/drift-live.txt

if diff -u /tmp/drift-expected.txt /tmp/drift-live.txt > /tmp/drift.diff 2>&1; then
  echo "✓ no drift — the live schema is exactly what migrations/ produces"
  echo "  ($(wc -l < /tmp/drift-expected.txt | tr -d ' ') schema facts compared)"
  exit 0
fi

echo ""
echo "✗ DRIFT DETECTED"
echo ""
echo "  '-' is what migrations/ produces; '+' is what the live database has."
echo "  A '+' line is a change made outside the migration history."
echo ""
grep -E '^[-+][^-+]' /tmp/drift.diff | head -40
echo ""
echo "  full diff: /tmp/drift.diff"
exit 1
