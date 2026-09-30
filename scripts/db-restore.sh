#!/usr/bin/env sh
# Restore a pg_dump custom-format archive by building a NEW database and swapping
# it in, rather than running pg_restore --clean over the live one.
#
# --clean does not work on this schema. It issues
#   ALTER TABLE ONLY <partition> DROP CONSTRAINT <pkey>
# for every partition, Postgres refuses to drop a constraint a partition inherits,
# and posting_observations is partitioned (PostgreSQL bug #16928). Restoring into
# an empty database needs no DROP at all, so the problem does not arise.
#
# The swap is ordered so no step can lose data: the live database is renamed aside
# before the restored one takes its name, and is dropped only after both renames
# succeed. If anything fails part-way, both databases still exist.
#
# Usage: COMPOSE="docker compose" scripts/db-restore.sh backups/<file>.dump
# Called by `make db-restore`, which takes a safety backup and stops the services
# first. Run directly only with the services stopped.
set -eu

file=$1
compose=${COMPOSE:-docker compose}

admin() {
	# shellcheck disable=SC2086 # $compose is a command with arguments.
	$compose exec -T postgres psql -U jobtrack -d postgres -v ON_ERROR_STOP=1 -q "$@"
}

admin -c "DROP DATABASE IF EXISTS jobtrack_restore WITH (FORCE)"
admin -c "CREATE DATABASE jobtrack_restore"

# --single-transaction: a restore that fails leaves an empty side database, never
# a partial one that the swap below could mistake for a good restore.
# shellcheck disable=SC2086
$compose exec -T postgres pg_restore -U jobtrack -d jobtrack_restore \
	--no-owner --single-transaction <"$file"

admin <<'SQL'
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
 WHERE datname IN ('jobtrack', 'jobtrack_restore') AND pid <> pg_backend_pid();
ALTER DATABASE jobtrack RENAME TO jobtrack_replaced;
ALTER DATABASE jobtrack_restore RENAME TO jobtrack;
DROP DATABASE jobtrack_replaced WITH (FORCE);
SQL
