# JobTrack — everything runs in containers.
#
# The only host prerequisites are Docker and git. No Go, no Node, no tool
# installs: `make` shells into the tools container for you.
#
#   make            list targets
#   make dev        start the whole stack with file watching
#   make check      everything CI runs — the definition of done

.DEFAULT_GOAL := help
SHELL := /bin/sh

COMPOSE := docker compose
TOOLS   := $(COMPOSE) run --rm --no-deps tools
# Targets that touch the database need it up and healthy first.
TOOLS_DB := $(COMPOSE) run --rm tools

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
SERVICES   := api ingestor matcher scheduler resume-parser migrate

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage: make <target>\n\n"} \
	  /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } \
	  /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@echo ""

##@ Development

.PHONY: dev
dev: ## Start the full stack with file watching
	$(COMPOSE) up --watch

.PHONY: up
up: ## Start the stack in the background
	$(COMPOSE) up -d

.PHONY: down
down: ## Stop the stack, keeping data
	$(COMPOSE) down

.PHONY: clean
clean: ## Stop the stack and destroy all data
	$(COMPOSE) down -v --remove-orphans

.PHONY: logs dev-logs
logs dev-logs: ## Tail logs from all services
	$(COMPOSE) logs -f

.PHONY: shell
shell: ## Open a shell in the tools container
	$(COMPOSE) run --rm --no-deps tools sh

.PHONY: psql
psql: ## Open psql against the dev database
	$(COMPOSE) exec postgres psql -U jobtrack -d jobtrack

##@ Quality gates

.PHONY: check
check: arch-check fmt-check vet lint test build-all web-test bench-budget ## Everything CI runs. THE definition of done
	@echo ""
	@echo "  ✓ check passed"

.PHONY: arch-check
arch-check: ## Architecture invariants: layering, SQL location, function length, ADR index, citations
	@python3 scripts/arch/check.py

.PHONY: arch-check-update
arch-check-update: ## Rewrite the invariant baselines, then READ THE DIFF
	@python3 scripts/arch/check.py --update
	@echo ""
	@echo "  Read the diff. A '-' line is debt paid off. A '+' line is new debt,"
	@echo "  and it needs a sentence in the commit message saying why."

.PHONY: fmt
fmt: ## Format all Go code
	$(TOOLS) "gofmt -w ./cmd ./internal"

.PHONY: fmt-check
fmt-check: ## Fail if any file is unformatted
	@$(TOOLS) "test -z \"\$$(gofmt -l ./cmd ./internal)\" || \
	  { echo 'unformatted files:'; gofmt -l ./cmd ./internal; exit 1; }"

.PHONY: vet
vet: ## Run go vet
	$(TOOLS) "go vet ./..."

.PHONY: lint
lint: ## Run static analysis
	$(TOOLS) "go vet ./... && go build ./..."

.PHONY: tidy
tidy: ## Tidy go.mod and fail if it changed
	$(TOOLS) "go mod tidy && git diff --exit-code go.mod go.sum"

##@ Tests

.PHONY: test
test: ## Unit tests with the race detector
	# CGO_ENABLED=1 because -race requires cgo. Production builds stay at 0.
	$(TOOLS) "CGO_ENABLED=1 go test -race -count=1 ./..."

.PHONY: test-short test-unit
test-short test-unit: ## Fast unit tests, no database, skips timing-sensitive ones
	$(TOOLS) "go test -short -count=1 ./..."

.PHONY: test-integration
test-integration: ## Integration tests against a real Postgres
	$(COMPOSE) up -d postgres
	$(TOOLS_DB) "CGO_ENABLED=1 go test -race -tags=integration -count=1 ./..."

.PHONY: test-cover
test-cover: ## Coverage report
	$(TOOLS) "go test -coverprofile=coverage.out -covermode=atomic ./... && \
	  go tool cover -func=coverage.out | tail -20"

##@ Frontend

WEB := docker run --rm -v "$(PWD)/web":/app -w /app node:22-alpine

.PHONY: web-install
web-install: ## Install frontend dependencies
	$(WEB) npm install --no-audit --no-fund

.PHONY: web-build
web-build: ## Build the frontend
	$(WEB) npm run build

.PHONY: web-test
web-test: ## Frontend unit tests
	$(WEB) npx vitest run

.PHONY: web-check
web-check: ## Type-check Svelte and TypeScript
	$(WEB) npx svelte-check --tsconfig ./tsconfig.json

.PHONY: bench-budget
bench-budget: web-build ## Enforce the frontend performance budget
	$(WEB) node scripts/check-budget.js

.PHONY: test-e2e
test-e2e: ## Playwright end-to-end tests against the running stack
	$(COMPOSE) up -d web
	# internal/ is mounted read-only so the suite can use the resume fixtures
	# that live beside the Go code. Copying them into web/tests/ would create a
	# second copy of a binary file, and the two would drift the first time one
	# was regenerated.
	docker run --rm --network jobtrack_default \
	  -v "$(PWD)/web":/app -w /app \
	  -v "$(PWD)/internal":/internal:ro \
	  -e E2E_BASE_URL=http://web:5173 \
	  mcr.microsoft.com/playwright:v1.62.1-noble \
	  npx playwright test --project=chromium

##@ Build

.PHONY: build-all
build-all: ## Compile every binary
	$(TOOLS) "go build ./..."

.PHONY: images
images: ## Build production images for every service
	@for s in $(SERVICES); do \
	  echo "--> $$s"; \
	  docker build --target $$s \
	    --build-arg VERSION=$(VERSION) \
	    --build-arg COMMIT=$(COMMIT) \
	    --build-arg BUILD_TIME=$(BUILD_TIME) \
	    -t jobtrack/$$s:$(VERSION) . || exit 1; \
	done
	@echo ""
	@docker images --filter=reference='jobtrack/*' \
	  --format 'table {{.Repository}}\t{{.Tag}}\t{{.Size}}'

##@ Migrations

.PHONY: migrate-up
migrate-up: ## Apply pending schema migrations
	$(TOOLS_DB) "go run ./cmd/migrate up"

.PHONY: migrate-status
migrate-status: ## Show applied and pending migrations
	$(TOOLS_DB) "go run ./cmd/migrate status"

.PHONY: migrate-verify
migrate-verify: ## Exit non-zero if the database is behind the binary
	$(TOOLS_DB) "go run ./cmd/migrate verify"

.PHONY: migrate-data
migrate-data: ## Run pending data migrations in the foreground
	$(TOOLS_DB) "go run ./cmd/migrate data"

.PHONY: migrate-repair
migrate-repair: ## List dirty migrations (add V=<version> to clear one)
	$(TOOLS_DB) "go run ./cmd/migrate repair $(V)"

.PHONY: migrate-new
migrate-new: ## Create a migration: make migrate-new NAME=add_foo
	@test -n "$(NAME)" || { echo "usage: make migrate-new NAME=add_foo"; exit 1; }
	@next=$$(ls migrations/*.up.sql 2>/dev/null | sed 's|.*/||' | cut -d_ -f1 | sort -n | tail -1); \
	  next=$$(printf '%04d' $$((10#$${next:-0} + 1))); \
	  f="migrations/$${next}_$(NAME).up.sql"; \
	  printf -- '-- %s\n--\n-- Must be backward-compatible with the currently deployed release:\n-- during a rolling deploy both versions run against this schema.\n-- Expand now, contract in a later release.\n--\n-- Add `-- +migrate no-transaction` above if this needs CREATE INDEX\n-- CONCURRENTLY or ALTER TYPE ... ADD VALUE.\n\n' "$(NAME)" > $$f; \
	  echo "created $$f"

##@ Data

.PHONY: seed
seed: ## Load realistic development data (derived from adapter golden fixtures)
	$(TOOLS_DB) "go run ./cmd/seed"

.PHONY: seed-reset
seed-reset: ## Wipe and reseed postings
	$(TOOLS_DB) "go run ./cmd/seed -reset"

.PHONY: seed-large
seed-large: ## Generate a large corpus for query-plan work
	$(TOOLS_DB) "go run ./cmd/seed -count=200000"

##@ Migrations (cont.)

.PHONY: db-reset
db-reset: ## Drop, recreate and migrate the dev database
	$(COMPOSE) down -v postgres
	$(COMPOSE) up -d postgres
	@until $(COMPOSE) exec -T postgres pg_isready -U jobtrack -d jobtrack >/dev/null 2>&1; do sleep 1; done
	$(MAKE) migrate-up

##@ Not yet implemented
.PHONY: generate
generate: ## Regenerate TypeScript API types from api/openapi.yaml
	@echo "generating TypeScript types from api/openapi.yaml..."
	@docker compose run --rm --no-deps -T web \
	  npx --yes openapi-typescript@7 /spec/openapi.yaml -o src/lib/api.d.ts
	@echo "wrote web/src/lib/api.d.ts"

.PHONY: check-generated
check-generated: ## CI gate: fail if the committed types differ from the spec
	@# Regenerates and compares, rather than trusting that whoever changed the
	@# spec remembered to run `make generate`. A gate that assumes the thing it
	@# is checking has already been done is not a gate.
	@#
	@# Separate from `generate` on purpose: chaining them would make the
	@# generate target fail every time it did its job, which teaches people to
	@# ignore it.
	@test -d .git || { echo "not a git repository; skipping"; exit 0; }
	@cp web/src/lib/api.d.ts /tmp/api.d.ts.committed 2>/dev/null || true
	@$(MAKE) --no-print-directory generate >/dev/null
	@diff -q /tmp/api.d.ts.committed web/src/lib/api.d.ts >/dev/null 2>&1 || { \
	  echo "generated types are out of date — run 'make generate' and commit the result"; \
	  exit 1; }
	@echo "generated types match the spec"


.PHONY: coverage
coverage: ## Report how much of the live corpus the skill extractor can read (ADR-0009)
	$(COMPOSE) run --rm --no-deps -T \
	  -e DATABASE_URL="postgres://jobtrack:dev@postgres:5432/jobtrack?sslmode=disable" \
	  tools "go run ./cmd/covcheck"

# One URL per vendor. This used to be a single hard-coded Greenhouse URL with a
# VENDOR variable that only chose the output directory, so capturing an Ashby
# fixture wrote a Greenhouse response into the Ashby testdata folder — a fixture
# that looks captured, is committed forever, and tests the wrong parser.
CAPTURE_URL_greenhouse = https://boards-api.greenhouse.io/v1/boards/$(BOARD)/jobs?content=true
CAPTURE_URL_ashby = https://api.ashbyhq.com/posting-api/job-board/$(BOARD)?includeCompensation=true
CAPTURE_URL_smartrecruiters = https://api.smartrecruiters.com/v1/companies/$(BOARD)/postings?limit=100&offset=0
CAPTURE_URL = $(CAPTURE_URL_$(VENDOR))

.PHONY: capture-source
capture-source: ## Capture a live ATS response as a fixture: make capture-source VENDOR=greenhouse BOARD=stripe
	@test -n "$(VENDOR)" -a -n "$(BOARD)" || \
	  { echo "usage: make capture-source VENDOR=greenhouse BOARD=<board-token>"; exit 1; }
	@test -n "$(CAPTURE_URL)" || \
	  { echo "no capture URL for vendor '$(VENDOR)'; add one to CAPTURE_URL in the Makefile"; exit 1; }
	@echo "fetching $(VENDOR)/$(BOARD)..."
	@mkdir -p "internal/source/$(VENDOR)/testdata"
	@curl -sS -H 'User-Agent: JobTrackBot/1.0 (+https://jobtrack.dev/bot)' \
	  "$(CAPTURE_URL)" \
	  | python3 -m json.tool > "internal/source/$(VENDOR)/testdata/$(BOARD).json"
	@echo "wrote internal/source/$(VENDOR)/testdata/$(BOARD).json"
	@echo ""
	@echo "REVIEW IT BEFORE COMMITTING: strip anything resembling personal data."
	@echo "Some feeds carry recruiter names and contact addresses, and a fixture"
	@echo "is committed to the repository forever."

.PHONY: test-golden
test-golden: ## Source adapter golden-file tests (add UPDATE=1 to regenerate)
	@if [ "$(UPDATE)" = "1" ]; then \
	  echo "regenerating golden files — READ THE DIFF before committing"; \
	  $(TOOLS) "go test ./internal/source/... -update"; \
	else \
	  $(TOOLS) "go test ./internal/source/... -count=1"; \
	fi

.PHONY: ui-audit
ui-audit: ## Measurable rendering checks on every page at every width
	docker run --rm --network jobtrack_default \
	  -v "$(PWD)/web":/app -w /app \
	  -e E2E_BASE_URL=http://web:5173 \
	  mcr.microsoft.com/playwright:v1.62.1-noble node tools/ui-audit.mjs

.PHONY: screenshots
screenshots: ## Screenshot every page in both themes into web/.screenshots/
	@mkdir -p web/.screenshots
	docker run --rm --network jobtrack_default \
	  -v "$(PWD)/web":/app -w /app \
	  -v "$(PWD)/web/.screenshots":/shots \
	  -e E2E_BASE_URL=http://web:5173 \
	  -e PAGES="$(PAGES)" \
	  mcr.microsoft.com/playwright:v1.62.1-noble node tools/screenshot.mjs
	@echo "  -> web/.screenshots/"

.PHONY: test-scoring
test-scoring: ## Scoring engine tests — expected bands, never exact values
	$(TOOLS) "go test ./internal/matching/... ./internal/normalise/... -count=1 -v" \
	  | grep -E '^(=== RUN|--- (PASS|FAIL)|ok|FAIL)'

.PHONY: explain-score
explain-score: ## Print a stored score breakdown. EMAIL=you@example.com POSTING=123
	@if [ -z "$(EMAIL)" ]; then \
	  echo "usage: make explain-score EMAIL=grad@jobtrack.local [POSTING=<id>]"; \
	  echo ""; \
	  echo "Omit POSTING to see that user's top ten."; \
	  exit 2; \
	fi
	@$(COMPOSE) exec -T postgres psql -U jobtrack -d jobtrack -v ON_ERROR_STOP=1 \
	  -v email="'$(EMAIL)'" -v posting="$${POSTING:-0}" -f /dev/stdin < scripts/explain-score.sql


.PHONY: load-test
load-test: ## Load test the endpoints with latency budgets. PROFILE=smoke|full
	@# Runs inside the compose network so it hits the api service directly,
	@# measuring the server rather than the host's port forwarding.
	@#
	@# A caution worth repeating from phase-5 §10: the dashboard's slow query
	@# measured 68ms warm and 13,687ms under write churn — the SAME statement on
	@# the SAME data. A load test against an idle, freshly-vacuumed database
	@# reports the 68ms and tells you nothing. Run this while the ingestor is
	@# working if you want a number that means anything.
	docker run --rm --network jobtrack_default \
	  -v "$(PWD)/scripts/k6":/scripts \
	  -e BASE_URL=http://api:8080 \
	  -e PROFILE=$${PROFILE:-smoke} \
	  grafana/k6:latest run /scripts/feed.js

.PHONY: drift-check
drift-check: ## Compare the live schema against what migrations/ produces
	$(TOOLS_DB) "sh scripts/drift-check.sh"
