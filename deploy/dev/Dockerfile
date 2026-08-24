# Development image: the full Go toolchain plus the tools every make target
# needs. Deliberately separate from the production Dockerfile, which produces
# ~20 MB distroless images with no shell.
#
# Nothing is ever installed on a developer's host. If a command needs a tool,
# the tool lives here — which is what makes "Docker and git" the complete
# prerequisite list and what stops generated code differing between machines.

FROM golang:1.25-alpine

# git       : go mod, and VCS stamping in build info
# ca-certs  : outbound TLS
# make      : the entrypoint for every workflow
# postgresql-client : psql for `make psql` and debugging the queue
# curl      : probing services from inside the network
# poppler-utils : pdftotext, which the resume-parser shells out to. Present in
#             the dev image so `air` runs the same code path as production
#             rather than failing on a missing binary only in a container the
#             developer never builds.
# gcc, musl-dev : the race detector requires cgo. Tests run CGO_ENABLED=1;
#             production builds stay at 0 so the binary is static and can use
#             a distroless base.
RUN apk add --no-cache \
        git \
        ca-certificates \
        make \
        postgresql17-client \
        curl \
        bash \
        gcc \
        musl-dev \
        poppler-utils

# Tool versions are pinned. Unpinned tools are how two developers generate
# different code from the same source and spend an afternoon on the diff.
ENV AIR_VERSION=v1.61.7 \
    SQLC_VERSION=v1.27.0 \
    LEFTHOOK_VERSION=v1.10.10

RUN go install github.com/air-verse/air@${AIR_VERSION} && \
    go install github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION} && \
    go install github.com/evilmartians/lefthook@${LEFTHOOK_VERSION} && \
    rm -rf /root/.cache/go-build

WORKDIR /src

# Module cache and build cache are mounted as volumes by compose, so a rebuild
# of this image does not discard them.
# CGO off by default (matches production builds). Targets that need the race
# detector override it explicitly.
ENV GOFLAGS=-mod=mod \
    CGO_ENABLED=0
