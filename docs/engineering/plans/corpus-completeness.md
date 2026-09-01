# Corpus completeness

**Status:** partly done — the description fix shipped on 2026-09-01 and is still
sweeping. **Done when:** description coverage is stable above 90%, the same class
of bug is ruled out on every two-phase vendor, and the country gap is either
closed or explained.

## Where it stands

Measured 2026-09-01, mid-sweep:

| Fact | Known | Was |
|---|---|---|
| Description ≥ 200 chars | 66.9% | 64.1% and falling |
| Skills — at least one read | 52.2% | — |
| Work mode | 55.9% | — |
| Experience range | 54.5% | — |
| Country | **90.8%** | 61.1% before the parser fix |
| Pay | 15.0% | — |

The description figure is moving because the sweeps are running. Everything else
is static and is what this plan is about.

## Steps

1. **[opus]** Confirm the fix reached its ceiling. Wait for every SmartRecruiters
   board to report `detail_cursor = -1`, then measure. If coverage stalls below
   90%, the remaining gap is postings whose vendor genuinely publishes no body,
   and that is a different fact worth stating.
2. **[opus]** Rule out the same bug elsewhere. Greenhouse and Workday also have
   detail phases. The shared upsert fix covers all of them, but only
   SmartRecruiters was verified end to end. Check per vendor: a board whose
   coverage tracks `window ÷ board size` has the same disease.
3. **[sonnet]** Add the guard the fix deserves: an integration test asserting
   that upserting a posting with an empty body does not erase a stored one.
   The bug survived because nothing asserted this.
4. **[opus]** Country at 61% is the next largest gap and the cheapest to move —
   `internal/normalise` already parses locations, and the misses are mostly
   formats it has not been taught. Sample 100 unparsed `location_raw` values
   before writing any rule; the shape of the misses decides whether this is worth
   doing at all.
5. **[sonnet]** Once the parser changes, re-run the golden tests for every vendor
   and read the diff. Location strings are vendor-shaped, so a rule that fixes
   Workable can break Personio's multi-office join.
6. **[opus]** Skills at 52% is bounded by description coverage — a posting with
   no body yields no skills. Re-measure after step 1 rather than treating it as
   an independent problem, because most of it is not one.

## Traps

- **Coverage that tracks `window ÷ board size` is the signature.** Both bugs
  found on 2026-09-01 showed it, and neither showed anything else: no errors, no
  failing tests, no alerts.
- Pay at 15% is not a defect to fix. Employers do not publish it, and the
  absence field on the homepage exists to say so. Do not "improve" this number by
  inferring a salary — [principles](../../product/principles.md) forbids it and
  it is the single fastest way to make the product dishonest.
- A number moving after a fix is not proof the fix worked. Both of these were
  found while the corpus looked healthy from every angle the code could see.
