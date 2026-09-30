# Multi-target build for every Go service.
#
# One file rather than five near-identical ones: the build is byte-for-byte the
# same for each binary, and five copies would drift the moment one is updated.
# Each service still gets its own explicit final stage, so
# `docker build --target api` produces an image containing exactly one binary
# and nothing else. The frontend has its own Dockerfile because its build is
# genuinely different (web/Dockerfile).
#
#   docker build --target api            -t jobtrack/api .
#   docker build --target resume-parser  -t jobtrack/resume-parser .
#
# Final images are distroless/static: no shell, no package manager, no libc
# utilities. Chosen over `scratch` because distroless is rebuilt and patched
# upstream, so a CVE in the base is fixed by pulling a new tag — scratch has
# nothing to patch but also no maintenance stream.

# ---------------------------------------------------------------------------
# deps — cached separately so source edits do not re-download modules
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS deps
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# ---------------------------------------------------------------------------
# build — the compile environment, shared by every service stage
#
# It does NOT compile anything itself. Each service stage below runs its own
# `go build ./cmd/<name>`, which is more verbose than one parameterised build
# and is the point: `docker build --target api .` used to need
# `--build-arg SERVICE=api` alongside it, and passing a different one produced
# an image TAGGED api containing another service's binary, silently. A build
# argument that must agree with the target is a footgun, not a parameter.
#
# The caches are shared regardless: every stage mounts the same module and
# build caches, so the second service compiles against a warm cache.
# ---------------------------------------------------------------------------
FROM deps AS build
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

COPY . .

# CGO_ENABLED=0 produces a static binary, which is what allows a distroless
# static base. -trimpath keeps absolute build paths out of the binary, making
# the build reproducible and leaking nothing about the builder.
ENV CGO_ENABLED=0 GOOS=linux
ENV GOFLAGS="-trimpath"

# ---------------------------------------------------------------------------
# base — shared final layer contents
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime-base
# nonroot is uid/gid 65532. Declared explicitly so the Kubernetes
# securityContext and the image agree, rather than relying on the tag alone.
USER 65532:65532
WORKDIR /

# ---------------------------------------------------------------------------
# Service images
# ---------------------------------------------------------------------------

FROM build AS build-api
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w \
        -X github.com/ergodicregulus/jobtrack/internal/version.Version=${VERSION} \
        -X github.com/ergodicregulus/jobtrack/internal/version.Commit=${COMMIT} \
        -X github.com/ergodicregulus/jobtrack/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/app ./cmd/api
FROM runtime-base AS api
COPY --from=build-api /out/app /app
EXPOSE 8080
# No shell in the image, so the probe must be a real HTTP call from the
# orchestrator. Kubernetes httpGet handles this; there is deliberately no
# HEALTHCHECK using curl because curl does not exist here.
ENTRYPOINT ["/app"]

FROM build AS build-ingestor
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w \
        -X github.com/ergodicregulus/jobtrack/internal/version.Version=${VERSION} \
        -X github.com/ergodicregulus/jobtrack/internal/version.Commit=${COMMIT} \
        -X github.com/ergodicregulus/jobtrack/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/app ./cmd/ingestor
FROM runtime-base AS ingestor
COPY --from=build-ingestor /out/app /app
ENTRYPOINT ["/app"]

FROM build AS build-scheduler
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w \
        -X github.com/ergodicregulus/jobtrack/internal/version.Version=${VERSION} \
        -X github.com/ergodicregulus/jobtrack/internal/version.Commit=${COMMIT} \
        -X github.com/ergodicregulus/jobtrack/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/app ./cmd/scheduler
FROM runtime-base AS scheduler
COPY --from=build-scheduler /out/app /app
ENTRYPOINT ["/app"]

# ---------------------------------------------------------------------------
# poppler — pdftotext and exactly the shared libraries it needs
#
# Assembled here rather than installed into the final image so that image keeps
# no package manager and no shell. `ldd` resolves the closure once, at build
# time, and the result is copied in; a missing library then fails the BUILD
# rather than the first upload in production.
# ---------------------------------------------------------------------------
FROM debian:12-slim AS poppler
RUN apt-get update \
 && apt-get install -y --no-install-recommends poppler-utils \
 && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /poppler/lib \
 && cp /usr/bin/pdftotext /poppler/ \
 && ldd /usr/bin/pdftotext | awk '/=> \//{print $3}' | xargs -I{} cp -L {} /poppler/lib/ \
 && cp -L /lib64/ld-linux-x86-64.so.2 /poppler/lib/ 2>/dev/null || true

# resume-parser is the one service that processes untrusted binary input, so it
# is the one with the hardest runtime posture. The image contributes what it
# can — no shell, no package manager, non-root — and the deployment adds the
# rest: no network egress, no database credentials, read-only root, seccomp,
# memory cap.
#
# base-debian12 rather than static, because pdftotext needs a libc. That is a
# deliberate widening of this image's surface, taken because the alternative —
# implementing PDF text extraction in-process — puts font encodings and stream
# filters inside the service instead of inside a child process that can be
# killed. See ADR-0012.
FROM build AS build-resume-parser
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w \
        -X github.com/ergodicregulus/jobtrack/internal/version.Version=${VERSION} \
        -X github.com/ergodicregulus/jobtrack/internal/version.Commit=${COMMIT} \
        -X github.com/ergodicregulus/jobtrack/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/app ./cmd/resume-parser
FROM gcr.io/distroless/base-debian12:nonroot AS resume-parser
COPY --from=poppler /poppler/pdftotext /usr/bin/pdftotext
COPY --from=poppler /poppler/lib/ /usr/lib/
COPY --from=build-resume-parser /out/app /app
USER 65532:65532
EXPOSE 9090
ENTRYPOINT ["/app"]

# migrate runs as a Job to completion before a rollout, never as a Deployment.
FROM build AS build-migrate
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w \
        -X github.com/ergodicregulus/jobtrack/internal/version.Version=${VERSION} \
        -X github.com/ergodicregulus/jobtrack/internal/version.Commit=${COMMIT} \
        -X github.com/ergodicregulus/jobtrack/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/app ./cmd/migrate
FROM runtime-base AS migrate
COPY --from=build-migrate /out/app /app
ENTRYPOINT ["/app"]

# ---------------------------------------------------------------------------
# web — the SvelteKit server
# ---------------------------------------------------------------------------
# It had no image. docker-compose.prod.yml and deploy/k8s/web.yaml both run
# `web:${VERSION}`, and nothing in the build or release pipeline produced one,
# so neither deployment could serve a single page.
FROM node:22-alpine AS build-web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# package.json has no runtime dependencies — adapter-node bundles the app — so
# the image carries the build output and nothing from node_modules. Distroless,
# like the Go images: no shell, no package manager.
FROM gcr.io/distroless/nodejs22-debian12:nonroot AS web
WORKDIR /app
COPY --from=build-web /web/build ./build
COPY --from=build-web /web/package.json ./package.json
ENV NODE_ENV=production PORT=3000
USER 65532:65532
EXPOSE 3000
# The distroless entrypoint is node, so this runs `node build`.
CMD ["build"]
