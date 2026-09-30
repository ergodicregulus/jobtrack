# Matching and scoring

> **Rewritten from the code, 2026-09-30.** This page described a design that had drifted from what
> runs: a hot-reloadable YAML config that never existed, a semantic component that was never built,
> different weights, and a precomputation pipeline that [ADR-0016](adr/0016-scores-are-computed-not-materialised.md)
> removed. §1 and §4–§7 now follow `internal/matching/score.go` and `internal/store/feed.go`.

> Status: **DECIDED**. Scoring model: [ADR-0006](adr/0006-hybrid-retrieval-and-scoring.md).
> Retrieval: [ADR-0022](adr/0022-retrieval-is-lexical.md). Resume parsing:
> [ADR-0007](adr/0007-resume-parsing-local-first.md).

The scoring model is a set of named weights and thresholds, versioned and explained, rather than
constants scattered through code — so it can be tuned and every change leaves a record. It is not a
runtime-loaded file: a change is a reviewed diff and a deploy (§1 says why).

## 0. The governing constraint

From [principles.md §P3](../product/principles.md#p3--never-claim-precision-we-do-not-have):

> A match score of "87%" implies a calibrated probability. We do not have one, and neither does any
> competitor displaying such a number.

So the output of this system is not a number. It is:

```
Band          Strong fit
Score         80            (available on expand, not headline)
Components    skills 32/40 · experience 20/20 · location 15/15 · comp 8/15 · freshness 5/10
Gaps          Missing must-haves: Kafka, Terraform
Confidence    0.82          (resume parsed 0.91 · posting parsed 0.74)
```

Every one of those lines is renderable in the UI, and the last two are the ones competitors omit.

---

## 1. The model

A weighted sum over independent components. Deliberately **not** a learned end-to-end ranker,
because [P7](../product/principles.md#p7--explainability-is-a-feature-not-a-debug-tool) requires that
we can explain any ranking as text.

```
score = 100 × Σ earned_i / Σ max_i        over the components that did not abstain
```

A component that cannot judge — no salary stated, no location preference set — is **neutral** and
leaves both sums, so abstaining never drags a score down. Skills is the one exception, because its
abstention is the failure that let a sales role score 98 for a backend engineer: when it abstains it
is credited 0.25 of its weight, the corpus median, rather than removed
([ADR-0011](adr/0011-abstention-credit-calibration.md)). The band then comes from the score, tempered
by confidence (§8).

The defaults live in Go, in `matching.DefaultConfig()`, version `2026.08.7`:

| Component | Weight | Parameters |
|---|---|---|
| Skills | **40** | must-haves carry 75% of the component; an adjacent skill earns 0.5; abstention credit 0.25 |
| Experience | **20** | 4 points per year under the stated band, 8 per year over |
| Location | **15** | — |
| Compensation | **15** | undisclosed pay is neutral, never a penalty |
| Freshness | **10** | halves every 7 days |

Bands: **strong** ≥ 70, **plausible** ≥ 50, **stretch** ≥ 30, otherwise **unlikely**. A posting parsed
below 0.4 confidence is not scored at all.

**Why code and not a config file.** An earlier version of this document showed a hot-reloadable
`config/scoring/default.yaml`. It never existed. Weights are the part most likely to be wrong, and in
code a change to one arrives as a reviewed diff with the test corpus run against it — which is worth
more than reloading without a deploy. `DefaultConfig` carries a dated changelog of every change that
moved a number, and why.

---

## 2. Component: skills coverage

The largest component and the one that determines whether scores are useful at all.

**The must-have / nice-to-have split is the whole ballgame.** Treating every technology mentioned in a
job description as required produces scores that are uniformly low — every posting mentions fifteen
technologies and nobody has all fifteen — and a uniformly low score cannot rank anything.

Classification is **section-aware**, applied during ingestion:

| Section heading matches | Classification |
|---|---|
| `requirements`, `qualifications`, `you have`, `must have`, `basic qualifications` | `must_have` |
| `nice to have`, `bonus`, `preferred`, `plus`, `desirable` | `nice_to_have` |
| Anywhere else (intro, benefits, "our stack") | `mentioned` — scored at low weight |

```
skills_score = 40 × ( 0.75 × must_have_coverage + 0.25 × nice_to_have_coverage )

where coverage counts a skill as matched if:
  - exact canonical match, or
  - alias match (postgres → postgresql), or
  - adjacent match at partial_credit_adjacent (mysql → postgresql, 0.5)
```

**Adjacency comes from a curated table, not from embeddings.** Embedding similarity puts "Java" and
"JavaScript" close together, which is exactly the failure a candidate would be furious about.
Adjacency is hand-maintained per skill pair, sourced from the skill taxonomy, and reviewed. The
Fraunhofer duplicate-detection work reached the same conclusion in a neighbouring problem —
**curated weighted lookup lists for specific skills outperformed embeddings alone** `[A-20]`.

**Years-weighted matching:** a resume claiming 6 months of Kubernetes against a posting wanting 3
years scores partial, not full. Years come from `resume_skills.years`, derived by attributing skills
to dated employment periods.

---

## 3. Component: experience fit

```
if yoe_confidence < 0.5 or no minimum stated:   # posting's YoE claim is unreliable
    return neutral                               # do not penalise on our own parse failure
if user.total_yoe is unknown:
    return neutral

gap_under = max(0, posting.yoe_min - user.total_yoe)
gap_over  = max(0, user.total_yoe - posting.yoe_max)   # 0 when no maximum is stated

score = 20 - 4×gap_under - 8×gap_over    # floored at 0
```

Two deliberate asymmetries:

**Under-shooting is penalised half as much as over-shooting.** A large share of SDE-1 postings say
"2+ years", and that band is soft in practice — self-filtering there costs real opportunities
`[problem-statement §6](../product/problem-statement.md#6-the-india--bengaluru-layer)`. A 1-YOE
engineer applying to a "2+ years" role should see `plausible`, not `unlikely`. Meanwhile a
6-year engineer looking at an intern posting genuinely should be pushed down.

**Our own parse failure never costs the user.** If `yoe_confidence` is low, the component is
neutral rather than a guess. This rule recurs across every component and is the practical form of
P3: **uncertainty on our side is never converted into a penalty on the user's side.**

---

## 4. Components: location, compensation, freshness

**Location** (weight 15). Neutral until the user states a country or work arrangement. A remote
posting scores full marks for anyone who accepts remote; otherwise matching country and arrangement
score full, the right country with a different arrangement scores half, and anything else scores
zero. An unknown country or arrangement on the posting is not held against it.

**Compensation** (weight 15). The rule that matters:

```
if posting.comp_min is NULL:
    return neutral (full marks)   # undisclosed_treatment: neutral
```

Only ~80% of postings carry salary data `[A-12]`, and Ashby is the only ATS returning it
consistently. Penalising non-disclosure would systematically down-rank every Greenhouse and Lever
posting — an artefact of the *source*, not of the job. The UI distinguishes *"below your floor"* from
*"not disclosed"* rather than collapsing them.

**Freshness** (weight 10). Halves every 7 days: a posting from today earns 10, a week old earns 5,
two weeks old 2.5. Neutral when the posting date is unknown. It is part of the score, not a separate
multiplier on it — see §5.

*There is no semantic component.* ADR-0006 designed one — cosine similarity between résumé and
posting embeddings, weighted 20 — and it was never built; nothing generates embeddings. Its weight
was not redistributed; the five components above are the whole model.
[ADR-0022](adr/0022-retrieval-is-lexical.md) records the decision and what would reopen it.

---

## 5. Ranking

The **score** answers "how well do I fit this?". The **order** answers "what should I look at now?".
They are not the same question, and the product keeps them apart.

Freshness contributes at most 10 of the 100 points, so it separates close matches without overturning
fit. Recency as an *order* is a separate choice the reader makes: the feed opens newest-first, a
signed-in, onboarded user defaults to **Best match** (by score), and a search sorts by text relevance
(§7). Being early matters `[B-04]`; the newest-first default is how the product says so, rather than
by folding a large recency term into a number that claims to be about fit.

---

## 6. When scoring runs

**On every request, for the viewer, and never stored**
([ADR-0016](adr/0016-scores-are-computed-not-materialised.md)). The materialised design this section
used to describe — a `user_job_scores` table, `matcher` workers and a fan-out on every new posting —
reached 1,952 MB for 244 users and was removed.

`matching.Scorer.Score` does arithmetic on fields computed once at ingest (skills, years, location,
work mode, compensation — ADR-0022) and parses no text, so a request pays only for the arithmetic:

- **Orders that do not depend on a score** — newest, compensation, relevance — keep keyset pagination
  and score only the page returned: 25 rows, about 46 µs.
- **Best match, or a band filter**, must score the candidate set before it can rank it. It scores the
  newest 2,000 candidates that pass the filters (`scoreCandidateCap`).

A profile change takes effect on the next request. There is nothing to rescore and nothing to go stale.

---

## 7. Retrieval

Retrieval is **lexical** ([ADR-0022](adr/0022-retrieval-is-lexical.md)): full-text search over
`search_tsv`, weighted title **A** over description **B**, queried with `websearch_to_tsquery` and
ordered by `ts_rank` when a search is present, inside the structured filters — country, work mode,
field, experience band, compensation normalised to USD, vendor and recency.

`ts_rank` is a TF-IDF variant rather than true BM25, which is a real but acceptable limitation at our
corpus size. If lexical quality becomes the binding constraint, the upgrade path is a BM25 extension
(ParadeDB's `pg_search`) inside the same Postgres, not a separate search cluster.

The known weakness is vocabulary: a search for "backend" does not find a posting that says only
"server-side". ADR-0022 sets the measurement that would justify semantic retrieval, and notes that
extending the skill vocabulary is the cheaper fix if the misses turn out to be synonyms.

---

## 8. Confidence

Confidence is a first-class output, computed from both sides:

```
confidence = min(resume.parse_confidence, posting.parse_confidence)

if posting.parse_confidence < 0.4:
    do not score at all — show the posting with "insufficient detail to score"
```

Confidence is displayed, and it damps the score toward the band midpoint rather than being applied as
a silent multiplier — a low-confidence score should read as *uncertain*, not as *bad*.

In Go this is enforced by the type system rather than by convention:

```go
// Scored values never travel without their confidence. A function that returns
// a bare float64 score cannot exist in this package — the constructor is
// unexported and requires both.
type Scored struct {
    Value      float64
    Confidence float64
    Components []Component
}
```

---

## 9. Calibration and evaluation

A scoring model nobody evaluates drifts silently. Three mechanisms:

**Golden corpus.** ~200 hand-labelled resume/posting pairs with an expected *band*, never an exact
value. CI fails if band accuracy drops below 85%. Bands rather than values because exact scores are
not meaningful and testing against them produces brittle tests that get deleted.

**Distribution monitoring.** Score distribution per band, tracked over time. If `strong` grows from
5% to 30% of results, something regressed — most likely must-have classification. This catches more
real problems than the golden corpus does.

**Outcome feedback (v2+, and honestly labelled).** We know which scored postings the user applied to
and what happened. That is the only real ground truth, and it is heavily confounded — users apply to
what we rank highly, so outcomes partly measure our own ranking. It is used to detect gross
miscalibration, **not** to train an end-to-end ranker, because that would break explainability (P7).

If a learned re-ranker is ever added, it sits **on top of** the explainable base score and is capped
in how far it may move a result, so the explanation stays true.

---

## 10. What we deliberately do not do

| Not doing | Why |
|---|---|
| Show a headline percentage | [P3](../product/principles.md#p3--never-claim-precision-we-do-not-have) |
| Embedding-only matching | Cosine over documents rewards shared register over fit; caps out as a tie-breaker |
| Learned end-to-end ranking as the primary order | Unexplainable; violates [P7](../product/principles.md#p7--explainability-is-a-feature-not-a-debug-tool) |
| LLM scoring on the hot path | Cost, latency, non-determinism, and it cannot be tested against a golden corpus. Optional enrichment only, opt-in, [ADR-0007](adr/0007-resume-parsing-local-first.md) |
| Penalise postings with no salary | Punishes an artefact of the source vendor, not the job |
| Penalise the user for our parse failures | Uncertainty on our side never becomes a penalty on theirs |
| Hide postings that score badly | The user decides; we inform. Same rule as the knockout radar |
