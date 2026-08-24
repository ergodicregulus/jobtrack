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
# build — one compile per target, sharing the module and build caches
# ---------------------------------------------------------------------------
FROM deps AS build
ARG SERVICE
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

COPY . .

# CGO_ENABLED=0 produces a static binary, which is what allows a distroless
# static base. -trimpath keeps absolute build paths out of the binary, making
# the build reproducible and leaking nothing about the builder.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w \
        -X github.com/jobtrack/jobtrack/internal/version.Version=${VERSION} \
        -X github.com/jobtrack/jobtrack/internal/version.Commit=${COMMIT} \
        -X github.com/jobtrack/jobtrack/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/app ./cmd/${SERVICE}

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
FROM runtime-base AS api
COPY --from=build-api /out/app /app
EXPOSE 8080
# No shell in the image, so the probe must be a real HTTP call from the
# orchestrator. Kubernetes httpGet handles this; there is deliberately no
# HEALTHCHECK using curl because curl does not exist here.
ENTRYPOINT ["/app"]

FROM build AS build-ingestor
FROM runtime-base AS ingestor
COPY --from=build-ingestor /out/app /app
ENTRYPOINT ["/app"]

FROM build AS build-matcher
FROM runtime-base AS matcher
COPY --from=build-matcher /out/app /app
ENTRYPOINT ["/app"]

FROM build AS build-scheduler
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
FROM gcr.io/distroless/base-debian12:nonroot AS resume-parser
COPY --from=poppler /poppler/pdftotext /usr/bin/pdftotext
COPY --from=poppler /poppler/lib/ /usr/lib/
COPY --from=build-resume-parser /out/app /app
USER 65532:65532
EXPOSE 9090
ENTRYPOINT ["/app"]

# migrate runs as a Job to completion before a rollout, never as a Deployment.
FROM build AS build-migrate
FROM runtime-base AS migrate
COPY --from=build-migrate /out/app /app
ENTRYPOINT ["/app"]
