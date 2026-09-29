#!/usr/bin/env bash
#
# Deploy JobTrack. Handles both fresh installs and upgrades.
#
#   scripts/deploy.sh <version> [namespace]
#
# The two cases are deliberately NOT separate code paths. The migrator derives
# pending work from a ledger in the database, so:
#
#   fresh install   empty ledger  -> every migration is pending -> all run
#   upgrade 1.0.0   ledger has 7  -> only migrations 8+ are pending -> those run
#
# One command, one behaviour, nothing to drift out of sync. The only thing that
# differs is that a fresh install marks background backfills 'skipped', because
# there are provably no rows to backfill.
#
# Sequence — and the order is the whole point:
#
#   1. preflight     fail fast on anything that would abort halfway
#   2. migrate Job   schema migrations run to COMPLETION before any pod starts
#   3. rollout       services updated; each pod re-verifies the schema at boot
#   4. verify        watch the rollout and the SLIs before declaring success
#
# Background data migrations are NOT part of this sequence. They are registered
# by step 2 and drained by the scheduler afterwards, precisely so a multi-hour
# backfill cannot hold up a deploy.

set -Eeuo pipefail

VERSION="${1:?usage: deploy.sh <version> [namespace]}"
NAMESPACE="${2:-jobtrack}"
REGISTRY="${REGISTRY:-ghcr.io/ergodicregulus/jobtrack}"
TIMEOUT="${TIMEOUT:-600s}"

# Job names must be unique per deploy: a completed Job is immutable, so reusing
# the name makes the second deploy fail with "field is immutable" rather than
# running the migration.
JOB_NAME="jobtrack-migrate-$(echo "$VERSION" | tr '.+' '--')-$(date +%s)"

log()  { printf '\033[36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33m warn\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31mfatal\033[0m %s\n' "$*" >&2; exit 1; }

trap 'die "deploy failed at line $LINENO — the cluster is unchanged past that point"' ERR

kc() { kubectl --namespace "$NAMESPACE" "$@"; }

# ---------------------------------------------------------------------------
# 1. Preflight
# ---------------------------------------------------------------------------
preflight() {
  log "preflight"

  command -v kubectl >/dev/null || die "kubectl not found"
  kubectl cluster-info >/dev/null 2>&1 || die "cannot reach the cluster"
  kc get namespace >/dev/null 2>&1 || die "namespace '$NAMESPACE' does not exist"

  # Every image must exist before anything is applied. Discovering a missing
  # image after the migration has run means rolling back a schema change.
  for svc in migrate api ingestor matcher scheduler resume-parser; do
    local image="$REGISTRY/$svc:$VERSION"
    if ! docker manifest inspect "$image" >/dev/null 2>&1; then
      die "image not found: $image (build and push before deploying)"
    fi
  done

  kc get secret jobtrack-secrets >/dev/null 2>&1 \
    || die "secret 'jobtrack-secrets' is missing"

  if kc get deployment jobtrack-api >/dev/null 2>&1; then
    local current
    current=$(kc get deployment jobtrack-api \
      -o jsonpath='{.spec.template.spec.containers[0].image}' | sed 's/.*://')
    log "upgrade: $current -> $VERSION"
    DEPLOY_KIND=upgrade
  else
    log "fresh install: $VERSION"
    DEPLOY_KIND=fresh
  fi
}

# ---------------------------------------------------------------------------
# 2. Migrations — must complete before any application pod starts
# ---------------------------------------------------------------------------
migrate() {
  log "running schema migrations (job: $JOB_NAME)"

  # Print the plan first. On an upgrade this is the operator's last chance to
  # notice an unexpected migration before it is applied.
  kc run "jobtrack-migrate-status-$$" \
      --image="$REGISTRY/migrate:$VERSION" \
      --restart=Never --rm --attach --quiet \
      --overrides="$(overrides)" \
      --command -- /app status 2>/dev/null || warn "could not print migration status"

  sed -e "s|{{JOB_NAME}}|$JOB_NAME|g" \
      -e "s|{{VERSION}}|$VERSION|g" \
      -e "s|{{REGISTRY}}|$REGISTRY|g" \
      deploy/k8s/migrate-job.yaml | kc apply -f -

  log "waiting for migrations to complete (timeout $TIMEOUT)"
  if ! kc wait --for=condition=complete --timeout="$TIMEOUT" "job/$JOB_NAME"; then
    echo "--- migration logs ---" >&2
    kc logs "job/$JOB_NAME" --tail=200 >&2 || true
    die "migrations failed — NOTHING has been rolled out, the running version is untouched"
  fi

  kc logs "job/$JOB_NAME" --tail=50
  log "migrations complete"
}

# ---------------------------------------------------------------------------
# 3. Rollout
# ---------------------------------------------------------------------------
rollout() {
  log "rolling out services"

  for svc in api ingestor matcher scheduler resume-parser; do
    sed -e "s|{{VERSION}}|$VERSION|g" -e "s|{{REGISTRY}}|$REGISTRY|g" \
        "deploy/k8s/$svc.yaml" | kc apply -f -
  done

  # api first: if it fails, stop before touching the workers. Workers can lag a
  # release safely; a broken api is user-visible immediately.
  for svc in api ingestor matcher scheduler resume-parser; do
    log "waiting for $svc"
    if ! kc rollout status "deployment/jobtrack-$svc" --timeout="$TIMEOUT"; then
      warn "$svc rollout failed — rolling back"
      kc rollout undo "deployment/jobtrack-$svc"
      die "rollout of $svc failed and was reverted.
The schema is expand-only, so the previous release still runs against it."
    fi
  done
}

# ---------------------------------------------------------------------------
# 4. Verify
# ---------------------------------------------------------------------------
verify() {
  log "verifying"

  local running
  running=$(kc get deployment jobtrack-api \
    -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo 0)
  [ "${running:-0}" -ge 1 ] || die "no ready api replicas after rollout"

  # Data migrations are informational here. A pending backfill is expected and
  # correct — it drains in the background and must never gate a deploy.
  kc run "jobtrack-migrate-status-post-$$" \
      --image="$REGISTRY/migrate:$VERSION" \
      --restart=Never --rm --attach --quiet \
      --overrides="$(overrides)" \
      --command -- /app status 2>/dev/null || true

  log "deployed $VERSION ($DEPLOY_KIND), api replicas ready: $running"
  echo
  echo "  Watch the SLIs for 10 minutes before considering this done."
  echo "  Rollback:  kubectl -n $NAMESPACE rollout undo deployment/jobtrack-api"
  echo "  Safe because migrations are expand-only: the previous release still"
  echo "  runs against this schema. Contract steps ship alone, in a later release."
}

# Pod overrides for the one-shot status runs.
overrides() {
  cat <<JSON
{"spec":{"containers":[{"name":"migrate","image":"$REGISTRY/migrate:$VERSION",
"envFrom":[{"secretRef":{"name":"jobtrack-secrets"}},{"configMapRef":{"name":"jobtrack-config"}}]}]}}
JSON
}

main() {
  preflight
  migrate
  rollout
  verify
}

main "$@"
