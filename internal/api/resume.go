package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/jobs"
	"github.com/jobtrack/jobtrack/internal/resume"
)

// maxResumeBytes matches the limit documented in ADR-0007.
//
// Enforced here as well as by middleware because this handler is the one that
// hands bytes to a parser: a limit that lives only in a middleware chain is a
// limit that disappears the first time someone reorders the chain.
const maxResumeBytes = 5 << 20

// ResumeUpload is what the user gets back after uploading a CV.
//
// Deliberately NOT applied to the profile yet. Everything here is a proposal
// the user reviews and corrects first — a silently wrong skill list corrupts
// every score afterwards while looking like it worked, which is worse than
// having no skills at all.
type ResumeUpload struct {
	ID                int64               `json:"id"`
	Label             string              `json:"label"`
	Format            string              `json:"format"`
	Confidence        float64             `json:"confidence"`
	Email             string              `json:"email,omitempty"`
	Phone             string              `json:"phone,omitempty"`
	Links             []string            `json:"links,omitempty"`
	Skills            []ResumeSkill       `json:"skills"`
	YearsOfExperience *float64            `json:"years_of_experience"`
	Diagnostics       []resume.Diagnostic `json:"diagnostics"`

	// New reports which skills are not already on the profile. This is the
	// number that answers "was uploading this worth it?", and it is the one
	// the UI leads with.
	New int `json:"new_skills"`

	CreatedAt time.Time `json:"created_at"`
}

// ResumeSkill is one proposed skill with its evidence and whether it is new.
type ResumeSkill struct {
	Canonical string `json:"canonical"`
	// Label is how the skill should be written. The canonical form is the
	// identifier and is lower-case; showing it directly is what rendered
	// "postgresql" and "distributed systems" as chips on the review screen.
	Label    string   `json:"label"`
	Evidence string   `json:"evidence"`
	Years    *float64 `json:"years,omitempty"`
	// Known marks a skill the profile already has, so the UI can show what the
	// CV ADDS rather than a wall of things the user already told us.
	Known bool `json:"known"`
}

// errParserUnreachable separates "our parser is down" from "your file is
// broken". They need opposite messages: one is a temporary outage the user
// should retry, the other is a file they need to change.
var errParserUnreachable = errors.New("resume parser unreachable")

// resumeClient talks to the isolated parser service.
type resumeClient struct {
	url string
	hc  *http.Client
}

func newResumeClient(url string) *resumeClient {
	return &resumeClient{
		url: url,
		// 30s matches the parser's own budget in ADR-0007. A parse that has not
		// finished by then is a pathological file, and holding an API request
		// open behind it helps nobody.
		hc: &http.Client{Timeout: 30 * time.Second},
	}
}

type parseResponse struct {
	resume.Result
	Text string `json:"text"`
}

func (c *resumeClient) parse(ctx context.Context, body []byte) (*parseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.url+"/parse", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	res, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errParserUnreachable, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		// The parser speaks problem+json and its messages are written for the
		// user ("this file is most likely a scan"). Passing them through beats
		// replacing them with a generic failure.
		var p httpx.Problem
		if json.NewDecoder(res.Body).Decode(&p) == nil && p.Detail != "" {
			return nil, httpx.ErrUnprocessable(p.Detail)
		}
		return nil, fmt.Errorf("resume parser returned %d", res.StatusCode)
	}

	var out parseResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding parse result: %w", err)
	}
	return &out, nil
}

// handleResumeUpload accepts a CV, parses it, and stores the result for review.
func (a *API) handleResumeUpload(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	body, err := io.ReadAll(io.LimitReader(r.Body, maxResumeBytes+1))
	if err != nil {
		return httpx.ErrBadRequest("could not read the upload")
	}
	if len(body) == 0 {
		return httpx.ErrBadRequest("no file was uploaded")
	}
	if len(body) > maxResumeBytes {
		return httpx.ErrUnprocessable("That file is larger than 5 MB. A CV that size is " +
			"usually an image export, which applicant tracking systems cannot read either.")
	}

	parsed, err := a.parser.parse(ctx, body)
	if err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			return apiErr
		}
		a.log.ErrorContext(ctx, "resume parse failed", "error", err, "user_id", userID)

		// The parser being down is OUR outage and a temporary one, so the user
		// is told to try again rather than shown "internal server error" and
		// left wondering whether their CV is the problem. It is a separate
		// deployment precisely so it can fail alone; the error should reflect
		// that rather than implicate the whole product.
		if errors.Is(err, errParserUnreachable) {
			return httpx.ErrUnavailable("Our CV reader is temporarily unavailable. " +
				"Nothing is wrong with your file — please try again in a moment.")
		}
		return httpx.ErrInternal(err)
	}

	// The original file is deliberately NOT stored.
	//
	// ADR-0007 is privacy-driven, and the least risky place for a CV is nowhere.
	// We keep the extracted text (encrypted) and the structured parse, which is
	// everything the product needs — including re-running an improved parser
	// over the text later. What is lost is re-EXTRACTION from the original
	// bytes after an extractor change; that is a real cost, accepted in
	// exchange for not holding a bucket full of people's CVs.
	textEnc, err := a.crypt.SealString(parsed.Text)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	jsonBytes, err := json.Marshal(parsed.Result)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	jsonEnc, err := a.crypt.Seal(jsonBytes)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	label := r.URL.Query().Get("label")
	if label == "" {
		label = "Resume " + time.Now().Format("2 Jan 2006")
	}

	out := ResumeUpload{
		Label:             label,
		Format:            string(parsed.Format),
		Confidence:        parsed.Confidence,
		Email:             parsed.Email,
		Phone:             parsed.Phone,
		Links:             parsed.Links,
		YearsOfExperience: parsed.YearsOfExperience,
		Diagnostics:       parsed.Diagnostics,
		Skills:            []ResumeSkill{},
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	defer tx.Rollback(ctx)

	// is_default is set only when the user has no other CV. Silently
	// re-pointing the default at a file someone was merely trying out would
	// change every score they see without them asking.
	err = tx.QueryRow(ctx, `
		INSERT INTO resumes (user_id, label, is_default, blob_key, mime_type, byte_size,
		                     parsed_text_enc, parsed_json_enc, parse_confidence, parser_version)
		VALUES ($1, $2,
		        NOT EXISTS (SELECT 1 FROM resumes WHERE user_id = $1),
		        '', $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`,
		userID, label, mimeFor(parsed.Format), len(body),
		textEnc, jsonEnc, parsed.Confidence, parsed.ParserVersion).
		Scan(&out.ID, &out.CreatedAt)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return httpx.ErrInternal(err)
	}

	out.Skills, out.New, err = a.markKnownSkills(ctx, userID, parsed.Skills)
	if err != nil {
		return httpx.ErrInternal(err)
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusCreated, out)
	return nil
}

// markKnownSkills annotates each proposed skill with whether the profile has it.
//
// One query, not one per skill: a CV yields twenty skills and twenty round
// trips for a boolean is the definition of an N+1.
func (a *API) markKnownSkills(ctx context.Context, userID int64, found []resume.FoundSkill) ([]ResumeSkill, int, error) {
	out := make([]ResumeSkill, 0, len(found))
	if len(found) == 0 {
		return out, 0, nil
	}

	names := make([]string, len(found))
	for i, f := range found {
		names[i] = f.Canonical
	}

	// One query for both questions — "does the profile already have this" and
	// "how is it written" — because they are answered by the same two tables.
	rows, err := a.pool.Query(ctx, `
		SELECT s.canonical, s.display_name,
		       EXISTS (SELECT 1 FROM user_skills us
		                WHERE us.skill_id = s.id AND us.user_id = $1)
		  FROM skills s WHERE s.canonical = ANY($2)`, userID, names)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	known := map[string]bool{}
	labels := map[string]string{}
	for rows.Next() {
		var canonical, display string
		var have bool
		if err := rows.Scan(&canonical, &display, &have); err != nil {
			return nil, 0, err
		}
		known[canonical] = have
		labels[canonical] = display
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	newCount := 0
	for _, f := range found {
		label := labels[f.Canonical]
		if label == "" {
			// A skill the extractor found that is not yet a row in `skills`.
			// Falling back to the canonical is right — it is legible, and
			// inventing a casing rule here is how the two would drift.
			label = f.Canonical
		}
		s := ResumeSkill{
			Canonical: f.Canonical,
			Label:     label,
			Evidence:  f.Evidence,
			Years:     f.Years,
			Known:     known[f.Canonical],
		}
		if !s.Known {
			newCount++
		}
		out = append(out, s)
	}
	return out, newCount, nil
}

// ResumeApply is the user's decision about what to keep.
type ResumeApply struct {
	// Skills is the list the user CONFIRMED, not the list we proposed. The
	// client sends back what it is keeping, so an unchecked skill is never
	// written — corrections always win, per ADR-0007.
	Skills []string `json:"skills"`
	// SetYears applies the parsed years of experience to the profile. Opt-in
	// separately because it overwrites a field the user may have set by hand.
	SetYears bool `json:"set_years"`
}

// handleResumeApply writes the confirmed skills onto the profile.
func (a *API) handleResumeApply(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := httpx.UserIDFromContext(ctx)

	id, err := pathInt(r, "id")
	if err != nil {
		return err
	}

	var req ResumeApply
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	var jsonEnc []byte
	err = a.pool.QueryRow(ctx,
		`SELECT parsed_json_enc FROM resumes WHERE id = $1 AND user_id = $2`,
		id, userID).Scan(&jsonEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound()
	}
	if err != nil {
		return httpx.ErrInternal(err)
	}

	raw, err := a.crypt.Open(jsonEnc)
	if err != nil {
		return httpx.ErrInternal(fmt.Errorf("resume %d: %w", id, err))
	}
	var parsed resume.Result
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return httpx.ErrInternal(err)
	}

	// Only skills the parse actually found may be applied. Without this the
	// endpoint is an arbitrary "add any skill" API wearing a resume's name, and
	// the provenance recorded as origin='resume' would be a lie.
	proposed := map[string]bool{}
	for _, s := range parsed.Skills {
		proposed[s.Canonical] = true
	}
	keep := make([]string, 0, len(req.Skills))
	for _, s := range req.Skills {
		if proposed[s] {
			keep = append(keep, s)
		}
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return httpx.ErrInternal(err)
	}
	defer tx.Rollback(ctx)

	// Replace this resume's contribution wholesale rather than merging: the
	// user has just told us exactly what they want from it, and leaving behind
	// skills they unchecked would make the review meaningless.
	if _, err := tx.Exec(ctx,
		`DELETE FROM user_skills WHERE user_id = $1 AND origin = 'resume'`, userID); err != nil {
		return httpx.ErrInternal(err)
	}
	if len(keep) > 0 {
		// DO NOTHING on conflict, so a skill the user had already declared by
		// hand keeps origin='user'. Their own statement outranks our inference.
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_skills (user_id, skill_id, origin)
			SELECT $1, s.id, 'resume' FROM skills s WHERE s.canonical = ANY($2)
			ON CONFLICT (user_id, skill_id) DO NOTHING`, userID, keep); err != nil {
			return httpx.ErrInternal(err)
		}
	}

	if req.SetYears && parsed.YearsOfExperience != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE users SET total_yoe = $2, updated_at = now() WHERE id = $1`,
			userID, int(*parsed.YearsOfExperience+0.5)); err != nil {
			return httpx.ErrInternal(err)
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE resumes SET is_default = (id = $2), updated_at = now() WHERE user_id = $1`,
		userID, id); err != nil {
		return httpx.ErrInternal(err)
	}

	// Enqueued inside the transaction, so "skills saved" and "rescore queued"
	// are one atomic outcome. A commit that lands without the rescore would
	// leave every score stale against a profile that just changed materially.
	if _, err := a.river.InsertTx(ctx, tx, jobs.ScoreUserArgs{UserID: userID}, nil); err != nil {
		return httpx.ErrInternal(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return httpx.ErrInternal(err)
	}

	httpx.WriteJSON(ctx, w, a.log, http.StatusOK, map[string]any{
		"applied":   len(keep),
		"rescoring": true,
	})
	return nil
}

func mimeFor(f resume.Format) string {
	switch f {
	case resume.FormatDOCX:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case resume.FormatPDF:
		return "application/pdf"
	default:
		return "text/plain"
	}
}
