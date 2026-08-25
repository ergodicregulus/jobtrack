// Command covcheck reports how much of the live corpus the skill extractor can
// actually read.
//
// It exists because ADR-0009 turns on a number that has to be re-measured, not
// assumed. The vocabulary was expanded from 47 to ~120 terms on the strength of
// one measurement — 48.7% of postings yielding nothing — and the ADR commits to
// checking whether the ranking's correlation with skills overtakes its
// correlation with freshness afterwards. This is the instrument for that.
//
// Runs the extractor directly against stored descriptions rather than reading
// posting_skills, so it measures the CURRENT vocabulary without waiting for a
// re-ingest. The two agree once ingestion has caught up; a gap between them
// means postings are stale, which is itself worth knowing.
//
// It reports two populations separately, because they fail for different
// reasons and one used to hide the other: postings we could not READ (an
// ingestion problem) and postings we read and found nothing in (a vocabulary
// problem). The old query filtered the first group out entirely, so a vendor
// arriving with 1,861 empty descriptions moved the headline figure not at all
// while corpus-wide coverage went from 16% to 32%.
//
//	make coverage
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/normalise"
)

// sampleSize is a compromise. The whole corpus would be more accurate and takes
// minutes while ingestion is running; 1,500 postings is enough to move the
// headline percentages by well under a point.
const sampleSize = 1500

// readableMin is the length below which a description is a title and a link.
// Postings under it are not evidence about the vocabulary either way, so they
// are counted and reported rather than silently dropped.
const readableMin = 400

// coverage is what one run measured.
type coverage struct {
	live, unreadable     int // whole corpus
	sampled, zero        int // the sample
	skillTotal, withMust int
}

func (c coverage) readable() int { return c.live - c.unreadable }

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fatal("connect", err)
	}
	defer pool.Close()

	var c coverage
	if err := corpusTotals(ctx, pool, &c); err != nil {
		fatal("totals", err)
	}

	vocab := normalise.DefaultVocabulary()
	if err := sampleSkills(ctx, pool, vocab, &c); err != nil {
		fatal("sample", err)
	}
	if c.sampled == 0 {
		fmt.Println("no postings to sample — is the corpus seeded?")
		return
	}

	report(c, len(vocab.Canonicals()))
}

func fatal(what string, err error) {
	fmt.Fprintln(os.Stderr, what+":", err)
	os.Exit(1)
}

// corpusTotals counts every live posting first, so a body we never fetched is
// visible as itself rather than as an absence.
func corpusTotals(ctx context.Context, pool *pgxpool.Pool, c *coverage) error {
	return pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE coalesce(length(description_text), 0) <= $1)
		  FROM job_postings
		 WHERE status = 'live'`, readableMin).Scan(&c.live, &c.unreadable)
}

// sampleSkills extracts over a reproducible sample of readable postings.
//
// Ordered by a hash of the id, not by the id. `ORDER BY id LIMIT n` is not a
// sample — it is the oldest n rows, which meant the first board ever ingested,
// and every vendor added afterwards was invisible here. Hashing keeps the run
// reproducible while spreading the sample across the corpus.
func sampleSkills(
	ctx context.Context, pool *pgxpool.Pool, vocab *normalise.Vocabulary, c *coverage,
) error {
	rows, err := pool.Query(ctx, `
		SELECT description_text
		  FROM job_postings
		 WHERE status = 'live' AND length(description_text) > $2
		 ORDER BY md5(id::text)
		 LIMIT $1`, sampleSize, readableMin)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var description string
		if err := rows.Scan(&description); err != nil {
			return err
		}
		c.sampled++

		skills := vocab.ExtractSkills(description)
		c.skillTotal += len(skills)
		if len(skills) == 0 {
			c.zero++
		}
		for _, s := range skills {
			if s.Requirement == normalise.MustHave {
				c.withMust++
				break
			}
		}
	}
	return rows.Err()
}

func pct(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return 100 * float64(n) / float64(of)
}

func report(c coverage, canonicals int) {
	readable := c.readable()

	fmt.Printf("vocabulary          %d canonical skills\n", canonicals)
	fmt.Println()
	fmt.Printf("live postings       %d\n", c.live)
	fmt.Printf("  no body to read   %d (%.1f%%)  — ingestion, not the vocabulary\n",
		c.unreadable, pct(c.unreadable, c.live))
	fmt.Printf("  readable          %d (%.1f%%)\n", readable, pct(readable, c.live))
	fmt.Println()
	fmt.Printf("sampled             %d of the %d readable\n", c.sampled, readable)
	fmt.Printf("zero skills found   %d (%.1f%% of the sample)\n", c.zero, pct(c.zero, c.sampled))
	fmt.Printf("average per posting %.2f\n", float64(c.skillTotal)/float64(c.sampled))
	fmt.Printf("with a must-have    %d (%.1f%% of the sample)\n", c.withMust, pct(c.withMust, c.sampled))
	fmt.Println()

	// A posting with no body yields no skills, so corpus-wide coverage is the
	// unreadable share plus the sampled zero rate over the rest. Stated as a
	// projection because only the sample was extracted, not the whole corpus.
	projected := pct(c.unreadable, c.live) +
		pct(c.zero, c.sampled)*float64(readable)/float64(max(c.live, 1))
	fmt.Printf("projected corpus-wide zero-skill share: %.1f%%\n", projected)
	fmt.Println("  (unreadable postings + the sampled zero rate across the readable ones)")
	fmt.Println()
	fmt.Println("Baseline before the ADR-0009 expansion, for comparison:")
	fmt.Println("  vocabulary 47 · zero skills 48.7% · average 1.71 · with must-have 9.8%")
}
