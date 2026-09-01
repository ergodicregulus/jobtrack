package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/normalise"
	"github.com/jobtrack/jobtrack/internal/source"
)

// Posting is a normalised job posting ready to be written.
type Posting struct {
	SourceID      int64
	CompanyID     int64
	ExternalID    string
	RequisitionID string

	Title           string
	TitleNormalised string
	DescriptionHTML string
	DescriptionText string

	ApplyURL   string
	PostingURL string

	LocationRaw string
	Country     string
	Region      string
	City        string
	Mode        normalise.WorkMode

	CompMin      *float64
	CompMax      *float64
	CompCurrency string
	CompPeriod   string
	CompSource   string

	YoEMin        *int16
	YoEMax        *int16
	YoEConfidence float64

	PostedAt           *time.Time
	PostedAtIsEstimate bool

	AIScreeningDisclosed *bool
	AIDisclaimer         string
	AIOptOutURL          string

	ParseConfidence float64
	Raw             []byte

	Skills []normalise.ExtractedSkill

	// What kind of work this is, and why we think so. See ADR-0018.
	Field           string
	FieldConfidence float64
	FieldBecause    string
}

// UpsertResult reports what an ingest run changed, so the caller can decide
// what downstream work to enqueue.
type UpsertResult struct {
	PostingID int64
	Created   bool
	Changed   bool
}

// UpsertPosting inserts or updates a posting.
//
// Identity is (source_id, external_id) — a natural key from the vendor — so the
// whole operation is idempotent. That matters because retries are guaranteed:
// a worker killed by an autoscaler mid-batch re-runs the same postings.
//
// Runs inside the caller's transaction so the posting write and the jobs it
// triggers commit together. There is no window where a posting exists without
// its scoring job, which is the entire reason this system needs no outbox.
// upsertPostingSQL is the write path for every ingested posting.
//
// At package scope rather than inside UpsertPosting because it is 70 lines of
// SQL wrapped around 20 lines of Go, and burying it made the function read as
// though it were long when the only long thing is the statement. Same reason
// postingScoringColumns sits at package scope in feed.go.
const upsertPostingSQL = `
INSERT INTO job_postings (
    source_id, company_id, external_id, requisition_id,
    title, title_normalised, description_html, description_text,
    apply_url, posting_url,
    location_raw, country, region, city, mode,
    comp_min, comp_max, comp_currency, comp_period, comp_src,
    yoe_min, yoe_max, yoe_confidence,
    posted_at, posted_at_is_estimate,
    ai_screening_disclosed, ai_disclaimer, ai_opt_out_url,
    parse_confidence, raw,
    field, field_confidence, field_because,
    status, last_seen_at, missing_count
) VALUES (
    $1,$2,$3,NULLIF($4,''),
    $5,$6,$7,$8,
    $9,NULLIF($10,''),
    NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),$15,
    $16,$17,NULLIF($18,''),NULLIF($19,''),$20,
    $21,$22,$23,
    $24,$25,
    $26,NULLIF($27,''),NULLIF($28,''),
    $29,$30,
    $31,$32,$33,
    'live', now(), 0
)
ON CONFLICT (source_id, external_id) DO UPDATE SET
    requisition_id  = EXCLUDED.requisition_id,
    title           = EXCLUDED.title,
    title_normalised = EXCLUDED.title_normalised,
    -- AN EMPTY BODY NEVER ERASES ONE WE ALREADY HOLD.
    --
    -- Two-phase vendors serve descriptions from a per-posting endpoint, and the
    -- detail phase is bounded per poll (SmartRecruiters: 250). Every poll
    -- re-upserts the WHOLE board, so before this, each poll wrote a real body
    -- for the 250 in the window and blanked the other few thousand. The corpus
    -- could never accumulate: coverage settled at roughly window/board_size.
    --
    -- Measured on 2026-09-01, before the fix: Swiggy (41 postings) 87.8% had a
    -- body, Wise (559) 15.6%, BoschGroup (3,995) 1.9%. That is not a fetch
    -- failure — the detail endpoint returns 200 with 7 KB — it is this line
    -- overwriting good data with an absence, once every two hours.
    --
    -- It cost 4,604 of SmartRecruiters' 4,802 live postings their description,
    -- which is 36% of the whole corpus scoring as an ADR-0011 abstention rather
    -- than on its merits. Nothing alerted, because a missing body is a state the
    -- scorer handles honestly and the adapter treats a failed detail fetch as
    -- acceptable — so the one thing nobody checked was whether we were doing it
    -- to ourselves.
    description_html = CASE
        WHEN EXCLUDED.description_html <> '' THEN EXCLUDED.description_html
        ELSE job_postings.description_html
    END,
    description_text = CASE
        WHEN EXCLUDED.description_text <> '' THEN EXCLUDED.description_text
        ELSE job_postings.description_text
    END,
    apply_url       = EXCLUDED.apply_url,
    posting_url     = EXCLUDED.posting_url,
    location_raw    = EXCLUDED.location_raw,
    country         = EXCLUDED.country,
    region          = EXCLUDED.region,
    city            = EXCLUDED.city,
    mode            = EXCLUDED.mode,
    comp_min        = EXCLUDED.comp_min,
    comp_max        = EXCLUDED.comp_max,
    comp_currency   = EXCLUDED.comp_currency,
    comp_period     = EXCLUDED.comp_period,
    comp_src        = EXCLUDED.comp_src,
    yoe_min         = EXCLUDED.yoe_min,
    yoe_max         = EXCLUDED.yoe_max,
    yoe_confidence  = EXCLUDED.yoe_confidence,
    -- posted_at is never overwritten once we hold a non-estimate value.
    -- The vendor's updated_at moves on every edit; letting it replace a real
    -- first_published would make an edited 40-day-old posting look fresh,
    -- which corrupts the one signal this product is built on.
    posted_at = CASE
        WHEN job_postings.posted_at_is_estimate OR job_postings.posted_at IS NULL
            THEN EXCLUDED.posted_at
        ELSE job_postings.posted_at
    END,
    posted_at_is_estimate = CASE
        WHEN job_postings.posted_at_is_estimate OR job_postings.posted_at IS NULL
            THEN EXCLUDED.posted_at_is_estimate
        ELSE false
    END,
    ai_screening_disclosed = EXCLUDED.ai_screening_disclosed,
    ai_disclaimer   = EXCLUDED.ai_disclaimer,
    ai_opt_out_url  = EXCLUDED.ai_opt_out_url,
    -- Follows the body, because it is derived from it. Keeping a description
    -- while taking the confidence computed for an empty one would leave the row
    -- readable and permanently below the scoring floor.
    -- Re-classified on every poll: the classifier improves, and a posting
    -- stored under an older vocabulary should get the benefit of a newer one
    -- without needing a backfill each time.
    field            = EXCLUDED.field,
    field_confidence = EXCLUDED.field_confidence,
    field_because    = EXCLUDED.field_because,
    parse_confidence = CASE
        WHEN EXCLUDED.description_text <> '' THEN EXCLUDED.parse_confidence
        ELSE greatest(job_postings.parse_confidence, EXCLUDED.parse_confidence)
    END,
    raw             = EXCLUDED.raw,
    -- Seeing a posting again resurrects it: a role that came back is live
    -- again, and the miss counter must reset or it would close prematurely.
    status          = 'live',
    closed_at       = NULL,
    missing_count   = 0,
    last_seen_at    = now(),
    updated_at      = now()
RETURNING id, (xmax = 0) AS created,
          (job_postings.updated_at = job_postings.created_at) AS untouched`

func UpsertPosting(ctx context.Context, tx pgx.Tx, p Posting) (UpsertResult, error) {
	var res UpsertResult
	var untouched bool
	err := tx.QueryRow(ctx, upsertPostingSQL,
		p.SourceID, p.CompanyID, p.ExternalID, p.RequisitionID,
		p.Title, p.TitleNormalised, p.DescriptionHTML, p.DescriptionText,
		p.ApplyURL, p.PostingURL,
		p.LocationRaw, p.Country, p.Region, p.City, string(p.Mode),
		p.CompMin, p.CompMax, p.CompCurrency, p.CompPeriod, nullableCompSource(p.CompSource),
		p.YoEMin, p.YoEMax, p.YoEConfidence,
		p.PostedAt, p.PostedAtIsEstimate,
		p.AIScreeningDisclosed, p.AIDisclaimer, p.AIOptOutURL,
		p.ParseConfidence, p.Raw,
		p.Field, p.FieldConfidence, p.FieldBecause,
	).Scan(&res.PostingID, &res.Created, &untouched)
	if err != nil {
		return res, fmt.Errorf("upsert posting %s/%s: %w", p.ExternalID, p.Title, err)
	}

	res.Changed = res.Created || !untouched
	return res, nil
}

// nullableCompSource maps "" to NULL so the enum column stays clean.
func nullableCompSource(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ReplacePostingSkills rewrites a posting's skills.
//
// Delete-then-insert rather than a diff: the set is small (typically under 20),
// and a diff would need three statements to save one. Inside the caller's
// transaction, so the posting and its skills are never observed out of step.
func ReplacePostingSkills(ctx context.Context, tx pgx.Tx, postingID int64, skills []normalise.ExtractedSkill) error {
	if _, err := tx.Exec(ctx, `DELETE FROM posting_skills WHERE posting_id = $1`, postingID); err != nil {
		return fmt.Errorf("clear posting skills: %w", err)
	}
	if len(skills) == 0 {
		return nil
	}

	// One statement for the whole set. A loop here would be an N+1 inside the
	// hot ingestion path.
	const q = `
INSERT INTO posting_skills (posting_id, skill_id, requirement, confidence)
SELECT $1, s.id, x.requirement::skill_requirement, x.confidence
  FROM unnest($2::text[], $3::text[], $4::real[]) AS x(canonical, requirement, confidence)
  JOIN skills s ON s.canonical = x.canonical
ON CONFLICT (posting_id, skill_id) DO UPDATE
    SET requirement = EXCLUDED.requirement,
        confidence  = EXCLUDED.confidence`

	names := make([]string, len(skills))
	reqs := make([]string, len(skills))
	confs := make([]float32, len(skills))
	for i, s := range skills {
		names[i] = s.Canonical
		reqs[i] = string(s.Requirement)
		confs[i] = float32(s.Confidence)
	}

	if _, err := tx.Exec(ctx, q, postingID, names, reqs, confs); err != nil {
		return fmt.Errorf("insert posting skills: %w", err)
	}
	return nil
}

// RecordObservation appends to the append-only observation log.
//
// This is what makes "is this posting still open?" answerable from observed feed
// behaviour rather than by guessing employer intent.
func RecordObservation(ctx context.Context, tx pgx.Tx, postingID int64, contentHash []byte) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO posting_observations (posting_id, content_hash)
		 VALUES ($1, $2)
		 ON CONFLICT (posting_id, observed_at) DO NOTHING`,
		postingID, contentHash)
	return err
}

// ReconcileAbsent marks postings that were not seen in a full poll.
//
// TWO consecutive misses, never one. A truncated vendor response would
// otherwise close an entire board — the failure mode that has embarrassed every
// aggregator which skipped this check.
//
// Callers MUST NOT invoke this after a 304 or a partial response: absence from
// a response we did not fully receive means nothing.
// absenceLimit is how many consecutive polls a posting may be missing from its
// board before it is closed.
//
// Two, at a 2-hour tier, is four hours of absence. Deliberately not one: a board
// that paginates unstably can drop a posting from one page and restore it on the
// next, and closing on a single miss would make the corpus flap.
const absenceLimit = 2

// ReconcileAbsent counts a poll against every posting the board no longer lists,
// and closes the ones that have been gone long enough.
//
// ONE UPDATE PER ROW, and that is the entire point of this shape. The previous
// version bumped the counter in a data-modifying CTE and then updated the same
// rows again in the outer statement:
//
//	WITH bumped AS (UPDATE ... RETURNING id, missing_count)
//	UPDATE job_postings p SET status='closed' FROM bumped b
//	 WHERE p.id = b.id AND b.missing_count >= 2
//
// PostgreSQL does not apply a second update to a row already modified by the
// same command — "only one of the modifications takes place, and it is not
// reliably possible to predict which one". The outer UPDATE matched, reported
// rows affected, and changed nothing. This function returned a closure count it
// had not performed for its entire life, and NOT ONE POSTING WAS EVER CLOSED:
// 2,020 sat absent from their boards, one of them for 29 consecutive polls,
// every one still reading as live. It is why 42% of the corpus was over 60 days
// old.
//
// The count is taken from RETURNING rather than from the command tag, because
// the command tag is exactly what lied before.
func ReconcileAbsent(ctx context.Context, tx pgx.Tx, sourceID int64, seenExternalIDs []string) (closed int64, err error) {
	const q = `
WITH touched AS (
    UPDATE job_postings
       SET missing_count = missing_count + 1,
           status = CASE WHEN missing_count + 1 >= $3
                         THEN 'closed'::posting_status ELSE status END,
           closed_at = CASE WHEN missing_count + 1 >= $3
                            THEN now() ELSE closed_at END,
           updated_at = now()
     WHERE source_id = $1
       AND status = 'live'
       -- COALESCE, because a nil slice arrives as NULL and x = ANY(NULL) is
       -- NULL, so NOT NULL matches nothing: a board that legitimately emptied
       -- would count no absences and close no postings, silently. An empty array
       -- makes every live posting absent, which is the correct reading — the
       -- adapter's ErrSuspiciousEmpty guard is what decides whether an empty
       -- board is believable, and by this point it has.
       AND NOT (external_id = ANY(COALESCE($2::text[], '{}')))
    RETURNING status
)
SELECT count(*) FILTER (WHERE status = 'closed') FROM touched`

	if err := tx.QueryRow(ctx, q, sourceID, seenExternalIDs, absenceLimit).Scan(&closed); err != nil {
		return 0, fmt.Errorf("reconcile absent postings: %w", err)
	}
	return closed, nil
}

// EnsureSkills inserts any canonical skills that do not exist yet.
//
// Called once at startup with the vocabulary, so posting_skills can join on
// name without every ingest worker racing to create rows.
func EnsureSkills(ctx context.Context, pool *pgxpool.Pool, entries map[string]string) error {
	if len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	displays := make([]string, 0, len(entries))
	categories := make([]string, 0, len(entries))
	for name, category := range entries {
		names = append(names, name)
		displays = append(displays, normalise.DisplayName(name))
		categories = append(categories, category)
	}

	// display_name comes from the vocabulary rather than SQL initcap().
	// initcap produced "Aws", "Graphql", "Llm", "Node.Js" and "Postgresql" on
	// every card and chip in the product — getting a technology's name wrong on
	// screen reads as not knowing the field, to an audience that works in it.
	//
	// DO UPDATE, not DO NOTHING, so a correction to the table actually reaches
	// rows that already exist. With DO NOTHING the fix would only ever apply to
	// skills nobody had seen yet, which is the wrong half.
	_, err := pool.Exec(ctx, `
INSERT INTO skills (canonical, display_name, category)
SELECT x.name, x.display, x.category
  FROM unnest($1::text[], $2::text[], $3::text[]) AS x(name, display, category)
ON CONFLICT (canonical) DO UPDATE SET display_name = EXCLUDED.display_name`,
		names, displays, categories)
	if err != nil {
		return fmt.Errorf("ensure skills: %w", err)
	}
	return nil
}

// PostingFromRaw applies normalisation to a fetched posting.
//
// The bridge between the vendor-shaped world and the canonical one. Kept here
// rather than in the adapters so that every vendor gets identical treatment —
// an adapter that normalised its own dates would drift from the others.
func PostingFromRaw(raw source.RawPosting, src source.Source, vocab *normalise.Vocabulary) Posting {
	text := normalise.StripHTML(raw.DescriptionHTML)
	titleNorm, seniority := normalise.Title(raw.Title)
	loc := normalise.ParseLocation(raw.LocationRaw)
	yoeMin, yoeMax, yoeConf := normalise.YoE(text, seniority)

	p := Posting{
		SourceID:      src.ID,
		CompanyID:     src.CompanyID,
		ExternalID:    raw.ExternalID,
		RequisitionID: raw.RequisitionID,

		Title:           raw.Title,
		TitleNormalised: titleNorm,
		DescriptionHTML: raw.DescriptionHTML,
		DescriptionText: text,

		ApplyURL:   raw.ApplyURL,
		PostingURL: raw.PostingURL,

		LocationRaw: raw.LocationRaw,
		Country:     loc.Country,
		Region:      loc.Region,
		City:        loc.City,
		Mode:        normalise.Mode(raw.WorkplaceType, raw.LocationRaw, text),

		YoEMin:        yoeMin,
		YoEMax:        yoeMax,
		YoEConfidence: float64(yoeConf),

		PostedAt:           raw.PostedAt,
		PostedAtIsEstimate: raw.PostedAtIsEstimate,

		AIScreeningDisclosed: raw.AIScreeningDisclosed,
		AIDisclaimer:         raw.AIDisclaimer,
		AIOptOutURL:          raw.AIOptOutURL,

		Raw: raw.Raw,
	}

	// Structured vendor compensation always beats text extraction: it is a
	// published figure rather than something we read out of prose.
	if raw.CompIsStructured && raw.CompMin != nil {
		p.CompMin, p.CompMax = raw.CompMin, raw.CompMax
		p.CompCurrency, p.CompPeriod = raw.CompCurrency, raw.CompPeriod
		p.CompSource = "structured"
	} else if c := normalise.ParseCompensation(text); c.Disclosed() {
		p.CompMin, p.CompMax = c.Min, c.Max
		p.CompCurrency, p.CompPeriod = c.Currency, c.Period
		p.CompSource = c.Source
	}

	if vocab != nil {
		p.Skills = vocab.ExtractSkills(text)
	}

	// Classified here, after skills, because the skill count is half the
	// evidence. Doing it in the adapter would mean seven copies and no access to
	// the vocabulary; doing it in SQL would put a judgement in a place that
	// cannot explain itself.
	guess := normalise.ClassifyField(raw.Title, len(p.Skills))
	p.Field, p.FieldConfidence, p.FieldBecause = string(guess.Field), guess.Confidence, guess.Because

	p.ParseConfidence = parseConfidence(p)
	return p
}

// parseConfidence summarises how much of the posting we actually understood.
//
// Surfaced to the user and used to gate scoring: a posting we barely parsed
// should say so rather than be scored on fragments.
func parseConfidence(p Posting) float64 {
	score, total := 0.0, 5.0
	if p.Title != "" {
		score++
	}
	if len(p.DescriptionText) > 200 {
		score++
	}
	if p.Country != "" || p.Mode == normalise.ModeRemote {
		score++
	}
	if p.YoEConfidence >= float64(normalise.MinUsableConfidence) {
		score++
	}
	if len(p.Skills) > 0 {
		score++
	}
	return score / total
}
