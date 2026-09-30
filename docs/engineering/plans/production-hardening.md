# Plan — Production hardening after the first public release

**Status:** IN PROGRESS, started 2026-09-30. Done: steps 1–5 and 7 (backups and restore, corpus rebuilt live and backed up, ADR-0022, the production path assembled and smoke-tested in CI — ADR-0023, digest and consent tests — which found the digest ignoring the saved search). Remaining: 6 and 8. Written from a verification pass that
found the local corpus destroyed, one DECIDED ADR describing an unbuilt system, and
a release pipeline that has never booted its own images.

**Done when:** a database can be backed up and restored by one command each, and a
restore has been proven; the live corpus is rebuilt; no ADR marked DECIDED
describes code that does not exist; CI boots the release images and fails if they
do not answer `/readyz`; the digest path has an automated test; INP is gated in
CI; and every plan's status line matches the code.

## Why this order

Data safety first, because the corpus was lost to a `docker compose down -v` with no
backup to restore, and every corpus plan is blocked until data exists again. Then the
public README's truthfulness, because this repository is linked from a résumé. Then
the gates that would have caught today's bugs earlier.

## Steps

1. **[opus]** `make db-backup` / `make db-restore`: `pg_dump -Fc` into a gitignored
   `backups/`, restore with `pg_restore --clean --if-exists`. Prove a round trip on
   an isolated compose project, never the user's.
2. **[opus]** Confirm `INGEST_LIVE_ALLOWLIST` is enforced by the worker, not only
   validated, then start live ingestion to rebuild the corpus. It reaches 98
   third-party hosts, so politeness settings are checked first.
3. **[opus]** ADR-0022 superseding ADR-0006's vector retrieval: retrieval is
   `tsvector`/`ts_rank` plus the curated skill table. Contract migration dropping
   the four tables nothing reads. Record where match-time work happens: posting
   features at ingest, user-specific scores at read (ADR-0016).
4. **[opus]** CI job that boots the release images from `docker-compose.prod.yml`
   and fails unless `/readyz` answers. The OTel schema-URL crash passed every
   existing job and was fatal at startup.
5. **[opus]** Digest integration test with a fake sender; handler tests for
   unsubscribe and consent.
6. **[opus]** Gate INP p75 in CI at the 200 ms budget.
7. **[sonnet]** Correct the stale plan status lines: the corpus-completeness index
   row, the digest-email "needs an opt-in UI control", and corpus-relevance's unmet
   done-condition.
8. **[opus]** Once the corpus is rebuilt, state the live software share on the
   homepage from a query, never a constant — corpus-relevance's done-condition.
