# ADR-0020 — No object storage until something stores an object

- **Status:** DECIDED
- **Date:** 2026-09-30
- **Amends:** [ADR-0003](0003-postgres-single-datastore.md), whose "plus object storage for
  blobs" carve-out this removes. The rest of ADR-0003 stands and is strengthened.
- **Decision drivers:** a dev-stack service with zero callers; a startup requirement for a value
  nothing reads; MinIO's images removed from Docker Hub, which broke `make dev` on every machine
  that had not already cached them

## Context

The design has always said Postgres plus S3-compatible object storage for blobs. `docker-compose.yml`
runs MinIO and a one-shot `mc` container to create the bucket; `internal/config` requires
`OBJECT_STORE_ENDPOINT` for `api` and `resume-parser` and refuses to start without it.

None of it is used. Measured on the tree, 2026-09-30:

| | |
|---|---|
| Go files reading `cfg.Object` | **0** — the struct is assigned in `Load` and read nowhere |
| S3 or MinIO client libraries in `go.mod` | **0** |
| Value written to `resumes.blob_key` | **`''`**, a literal empty string, on every insert |
| Where an uploaded CV actually goes | `parsed_text_enc` / `parsed_json_enc`, encrypted, in Postgres |

The original file is never persisted at all. It is parsed in memory, the parse result is encrypted
with the user's DEK, and the bytes are dropped.

**MinIO forced the timing.** MinIO stopped publishing free images in October 2025, archived the
Docker Hub repository in April 2026, and deleted `minio/minio` and `minio/mc` on 2026-09-11.
`quay.io/minio/minio` now answers 401 to an anonymous pull. Every machine that had pulled the image
before kept working from its local cache, which is why this surfaced on a CI runner rather than on a
developer's laptop — the first CI run this repository ever had, on the first push to a remote.

## Options

| Option | Cost | Verdict |
|---|---|---|
| **Swap MinIO for another S3-compatible image** (SeaweedFS, Garage, LocalStack) | A new image to pin, learn and maintain, chosen to serve a component with no callers | Rejected. Picking a replacement vendor for something nothing connects to is how a stack accumulates services nobody can explain |
| **Authenticate to a MinIO registry** | Every contributor and CI needs credentials to bring up a dev stack; a public repository cannot ask that | Rejected |
| **Remove it until something needs it** | `blob_key` stays as a column nothing writes to, and the day we store the original file we add a service back | **Chosen** |

## Decision

Delete `minio` and `minio-init` from the dev stack. Drop `OBJECT_STORE_ENDPOINT` from the required
set — the `ObjectStore` config block is removed entirely rather than left as an unread struct, and
`.env.example` loses the section. `deploy/k8s` and `docker-compose.prod.yml` lose the same variables.

`resumes.blob_key` stays. It is `NOT NULL DEFAULT ''` and costs nothing, removing it is a
contract migration for no benefit, and it is the natural place for a key if the original file is ever
retained.

CLAUDE.md's datastore rule becomes "Postgres is the only datastore" with no carve-out, which is what
the code has always done.

## Consequences

### Good

- `make dev` works on a clean machine again. That was broken for anyone cloning fresh, and nobody
  with a warm Docker cache could have discovered it.
- Two fewer containers, and one fewer required environment variable per service.
- The architecture statement and the code agree. A documented component with zero callers is the
  precise failure this repository's tooling exists to catch, and no check caught this one — the
  dead-code check looks at Go identifiers, and `ObjectStore` is assigned, just never read.

### Bad, and accepted

- **Re-adding it is real work**, not a config flip: a client, a dependency, bucket lifecycle,
  credentials in two compose files and the k8s config. We are trading a cheap option we were not
  using for a clean tree.
- **The privacy story is now implicit.** "We never keep your original CV" is currently true because
  nothing was built to keep it, not because a decision was recorded to drop it. This ADR is where
  that gets written down; a future feature to re-download the original file is a deliberate reversal
  and should be argued as one.
- A reader of older documents will find object storage described as part of the system. Those
  documents are updated in the same change, but the git history still shows it.

## Revisit

When there is a concrete reason to persist the uploaded file — a user-facing "download my original
CV", a re-parse against a newer parser version, or a support workflow that needs the source
document. At that point write the superseding ADR and pick the storage vendor then, against a real
requirement rather than a placeholder one.
