# ADR-0012 — PDF text via a poppler subprocess, in reading order

- **Status:** DECIDED
- **Date:** 2026-08-17
- **Decision drivers:** parse accuracy, blast radius of untrusted input, dependency budget
- **Implements the PDF branch of [ADR-0007](0007-resume-parsing-local-first.md). Corrects one of its assumptions.**

## Context

[ADR-0007](0007-resume-parsing-local-first.md) chose a deterministic local
pipeline for resumes and shipped DOCX and plain text. PDF was left refused
rather than done badly, because **PDF is what most people actually have** and a
mangled CV silently corrupts every score the user sees afterwards — the worst
failure this pipeline can have.

A PDF stores positioned glyphs on a canvas, not text. Reading one means
implementing cross-reference tables, object streams, compression filters, font
encodings and CMaps. Getting any of those subtly wrong produces output that
looks plausible and is wrong.

## Options

| | In-process Go library | Hand-written parser | **poppler subprocess** |
|---|---|---|---|
| `go.mod` entries | 1 | 0 | **0** |
| Encoding coverage | partial | whatever we build | **the reference implementation** |
| A crash takes down | the service | the service | **one child process** |
| A hang takes down | the service | the service | **nothing — it is killed** |
| Memory blow-up hits | the service | the service | **the child's own limit** |
| Final image | ~20 MB static | ~20 MB static | **83 MB, needs libc** |

## Decision

**Shell out to `pdftotext`, from the already-isolated `resume-parser` service.**

A subprocess is not a workaround here, it is the same argument that created this
service one level further in. ADR-0007 put PDF parsing in its own deployment
because it is the only component executing over untrusted binary input; running
it in a child process means a crash, a hang or an allocation storm dies with the
child rather than with the service that would then hold the result.

It also costs **zero dependencies**. `pdftotext` is 40 KB, is the most exercised
implementation of this job in existence, and is patched by a distribution rather
than by us.

### Reading order, NOT `-layout` — and this reverses an assumption in ADR-0007

ADR-0007 states that *"layout-aware linearisation is the key step"* and that
naive top-to-bottom extraction *"interleaves two-column resumes into nonsense"*.
That is the conventional wisdom. **Measured against a real two-column CV, it is
backwards for this input class.**

`pdftotext -layout` reproduces the *visual* arrangement:

```
Priya Raman                Summary
priya.raman@example.com    Backend engineer working on payments infrastructure
Skills
Go                         Senior Software Engineer, Razorpay
PostgreSQL                 Mar 2021 - Present
```

That is the interleaving — the sidebar merged into the body, line by line. Plain
`pdftotext` emits **document order**, which is the order the generator wrote the
text in, and keeps the sidebar whole and then the body whole. Every CV tool in
common use (Word, Google Docs, LaTeX, Chromium's print path) writes logical
order, so this holds for the overwhelming majority of real files.

Measured on the fixture in `internal/resume/testdata/two-column-cv.pdf`:

| | Reading order | `-layout` |
|---|---|---|
| Skills recovered | **7 of 7** | headings merged into body lines |
| Dated roles found | **both** | dates glued to sidebar entries |
| Years of experience | **8** | — |
| Parse confidence | **1.00** | — |

The ADR-0007 claim is not wrong about *what* goes wrong; it is wrong about
*which mode causes it*. Written down because the intuitive choice is the broken
one, and the next person will reach for `-layout` for exactly the reason
ADR-0007 gives.

### A real file, not a pipe

Poppler is handed a temp file rather than stdin. A PDF's xref table is at the
END of the file, so a reader must seek backwards; given a pipe it buffers the
stream itself and — under the long-running service, though not in a one-shot
run — failed with a bare `exit 1` and no message. A seekable file removes the
ambiguity for the cost of one write of at most 5 MB, and the file is removed on
every path because it holds someone's CV.

**This means the deployment needs a writable temp dir.** That is a small
in-memory `emptyDir` mounted at `/tmp`, not a writable root: the filesystem
stays read-only and the scratch space vanishes with the pod.

### `-q` is deliberately off

Poppler's quiet mode suppresses syntax errors as well as warnings, and those
errors are the only thing distinguishing *"this file is a scan"* from *"these
bytes arrived truncated"*. That distinction cost an hour during implementation
and was resolved only once the messages were visible. stderr is bounded at 4 KB.

## Consequences

### Good

- PDF works, which is most of the market for this feature.
- Zero new `go.mod` entries, so the dependency budget is untouched.
- A hostile PDF kills a child process with a 20-second deadline and a bounded
  output buffer. The service does not notice.
- Poppler CVEs are fixed by rebuilding on a patched base, not by us.

### Bad, and accepted

- **The image grows from ~20 MB static to 83 MB** and now needs a libc, so
  `resume-parser` moves from `distroless/static` to `distroless/base-debian12`.
  It keeps no shell, no package manager and non-root. `pdftotext` and exactly
  its `ldd` closure are copied from a Debian stage, so a missing library fails
  the **build** rather than the first upload in production.
- **A writable `/tmp` is now required.** Recorded here because a read-only root
  with no `emptyDir` produces a failure that looks like a corrupt PDF.
- **Scanned PDFs are still unsupported**, exactly as ADR-0007 says. They return
  a specific diagnosis rather than a shrug, because a file with no text layer
  fails employer systems too and "export a text-based PDF" is real advice.

## Revisit

If a meaningful share of uploads fail with `pdftotext exit != 0` — the exit code
is in the log line for this reason — the next step is `-layout` as a **fallback**
for files where reading order yields too little, not as the default. Revisit if
reading order recovers under **80%** of skills on a corpus of 50 real CVs.
