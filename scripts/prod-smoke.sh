#!/usr/bin/env bash
# Boot the release images with docker-compose.prod.yml and use them the way a
# user would: over HTTPS, through Caddy, with a real sign-up.
#
# Until this existed the production path had never run, and could not have. Its
# Postgres image was pinned to a digest that did not exist; APP_ENV was spelled
# ENV, so every production guard was off; nothing built the web image; the proxy
# sent /v1 to a server with no /v1 route; and no production database could learn
# which job boards to poll. Every one of those passed every other CI job.
#
# Runs in an isolated compose project, so it never touches a development stack.
# INGEST_ENABLED=false: it proves the stack boots and serves, and contacts no ATS.
set -Eeuo pipefail

project=jobtrack-prod-smoke
env_file=$(mktemp)
jar=$(mktemp)
compose() { docker compose -p "$project" -f docker-compose.prod.yml --env-file "$env_file" "$@"; }
cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		compose ps -a || true
		compose logs --no-color --tail=60 || true
	fi
	compose down -v --remove-orphans >/dev/null 2>&1 || true
	rm -f "$env_file" "$jar"
	exit "$status"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
expect() { # expect <description> <want> <got>
	printf '  %-52s %s\n' "$1" "$3"
	[ "$3" = "$2" ] || fail "$1: want $2, got $3"
}

echo "building release images"
for t in api ingestor scheduler resume-parser migrate web; do
	docker build -q --target "$t" --build-arg VERSION=smoke -t "jobtrack/${t}:smoke" . >/dev/null
done

# Secrets that satisfy the production guards: long, and not the dev- placeholder.
cat >"$env_file" <<EOF
REGISTRY=jobtrack
VERSION=smoke
POSTGRES_PASSWORD=$(openssl rand -hex 16)
SESSION_SECRET=$(openssl rand -hex 24)
RESUME_ENCRYPTION_KEY=$(openssl rand -hex 24)
JOBTRACK_HOST=localhost
INGEST_ENABLED=false
EOF

echo "booting docker-compose.prod.yml"
compose up -d --wait --pull never

for _ in $(seq 1 30); do curl -sk -o /dev/null https://localhost/ && break; sleep 1; done
code() { curl -sk -o /dev/null -w '%{http_code}' "$@"; }

echo "routing, through Caddy over HTTPS"
expect "/ reaches web" 200 "$(code https://localhost/)"
expect "/v1 reaches the api" 200 "$(code 'https://localhost/v1/jobs?limit=1')"
expect "/unsubscribe reaches the api (400: no token)" 400 "$(code https://localhost/unsubscribe)"
expect "/jobs renders, calling the api server-side" 200 "$(code https://localhost/jobs)"

echo "production configuration is actually in force"
# Captured, not piped: under pipefail, `docker logs | grep -q` fails exactly when
# grep matches, because grep exits early and docker logs dies of SIGPIPE.
api_logs=$(docker logs "$project-api-1" 2>&1)
ingestor_logs=$(docker logs "$project-ingestor-1" 2>&1)
grep -q '"env":"prod"' <<<"$api_logs" || fail "the api is not running with APP_ENV=prod"
grep -q '"msg":"boards reconciled"' <<<"$ingestor_logs" ||
	fail "the ingestor did not register the curated boards"
ssl=$(docker exec "$project-postgres-1" psql -U jobtrack -d jobtrack -tAc \
	"SELECT bool_and(s.ssl) FROM pg_stat_ssl s JOIN pg_stat_activity a USING (pid)
	  WHERE a.datname = 'jobtrack' AND a.pid <> pg_backend_pid()")
expect "every database connection uses TLS" t "$ssl"

echo "a sign-up, as a browser makes one"
signup() { # signup <origin>
	curl -sk -c "$jar" -o /dev/null -w '%{http_code}' -X POST https://localhost/signup \
		-H "Origin: $1" -H 'Accept: text/html' -H 'Content-Type: application/x-www-form-urlencoded' \
		--data-urlencode "email=smoke-$RANDOM@example.com" \
		--data-urlencode 'password=a-long-enough-smoke-password'
}
expect "a forged Origin is refused (CSRF)" 403 "$(signup https://evil.example)"
expect "sign-up redirects to onboarding" 303 "$(signup https://localhost)"
expect "without the cookie, onboarding redirects" 303 "$(code https://localhost/onboarding)"
expect "with it, onboarding renders (cookie reaches the api)" 200 "$(code -b "$jar" https://localhost/onboarding)"
expect "the api knows the session directly" 200 "$(code -b "$jar" https://localhost/v1/me/profile)"

echo "production smoke passed"
