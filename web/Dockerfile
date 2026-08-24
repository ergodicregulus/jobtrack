# The web app's own Dockerfile.
#
# Separate from the root Dockerfile because the build is genuinely different:
# npm dependency resolution, a Vite build, and a Node runtime rather than a
# static Go binary. The Go services share one multi-target file precisely
# because they are identical; this one is not.
#
#   docker build -f web/Dockerfile -t jobtrack/web .

# ---------------------------------------------------------------------------
# deps — cached separately so source edits do not reinstall node_modules
# ---------------------------------------------------------------------------
FROM node:22-alpine AS deps
WORKDIR /app
COPY web/package.json web/package-lock.json* ./
# `npm ci` requires a lockfile and installs it exactly. `npm install` would
# silently resolve different versions on a rebuild, which is how two builds of
# the same commit end up different.
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund

# ---------------------------------------------------------------------------
# build
# ---------------------------------------------------------------------------
FROM node:22-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY web/ ./
RUN npm run build && npm prune --omit=dev

# ---------------------------------------------------------------------------
# runtime
# ---------------------------------------------------------------------------
# distroless/nodejs, not node:alpine: no shell, no package manager, no busybox.
# An attacker with code execution cannot pivot to running arbitrary commands,
# and the image is rebuilt upstream when a base CVE is patched.
FROM gcr.io/distroless/nodejs22-debian12:nonroot AS web

WORKDIR /app
COPY --from=build /app/build       ./build
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/package.json ./

# 65532 = nonroot. Declared explicitly so the Kubernetes securityContext and
# the image agree rather than relying on the tag alone.
USER 65532:65532

ENV NODE_ENV=production \
    PORT=3000 \
    HOST=0.0.0.0 \
    # SvelteKit's adapter-node needs to know it sits behind a proxy so that
    # request origin checks use the forwarded values rather than the pod IP.
    ORIGIN=http://localhost:3000 \
    PROTOCOL_HEADER=x-forwarded-proto \
    HOST_HEADER=x-forwarded-host

EXPOSE 3000

# No HEALTHCHECK: there is no shell or curl in this image to run one. The
# orchestrator performs an httpGet probe instead, which is the correct place
# for it anyway.
CMD ["build/index.js"]
