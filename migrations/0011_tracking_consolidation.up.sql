-- Consolidate saved jobs onto the applications table, and let a user track an
-- application without first uploading a resume.
--
-- Two problems, both introduced by 0009.
--
-- First, 0009 added `saved_jobs` while `applications` already represented a
-- saved job as status='saved'. That is two sources of truth for one question,
-- and they drift the moment a user saves a job in one place and advances it in
-- the other: the feed would show a job as unsaved while the tracker showed it
-- in progress. Consolidating on `applications` also means saving a job and
-- later marking it applied is a status change rather than a migration between
-- tables, which is what the tracker UI actually needs.
--
-- Second, `applied_requires_attribution` demanded a resume_id for any status
-- past 'saved'. Knowing which resume produced which outcome is genuinely
-- valuable — it is the only way to answer "is my CV working" — but requiring it
-- makes the tracker unusable for the very common case of someone who applied
-- through a company portal and has not uploaded anything here. Migration 0010
-- already made scoring work without a resume; this finishes that thought.
-- Channel and applied_at stay required: they cost one click each and without
-- them the funnel statistics are meaningless.

-- Carry over anything already saved. Empty in practice — 0009 shipped in this
-- same development cycle — but a migration that silently discards user data
-- when it turns out not to be empty is not one worth writing.
INSERT INTO applications (user_id, posting_id, company_id, company_name, role_title, status, why_applied, created_at, last_activity_at)
SELECT s.user_id, s.posting_id, p.company_id, c.name, p.title, 'saved', s.note, s.created_at, s.created_at
  FROM saved_jobs s
  JOIN job_postings p ON p.id = s.posting_id
  JOIN companies c ON c.id = p.company_id
 WHERE NOT EXISTS (
	SELECT 1 FROM applications a
	 WHERE a.user_id = s.user_id AND a.posting_id = s.posting_id
 );

DROP TABLE IF EXISTS saved_jobs;

ALTER TABLE applications DROP CONSTRAINT IF EXISTS applied_requires_attribution;

ALTER TABLE applications ADD CONSTRAINT applied_requires_attribution
	CHECK (
		status IN ('saved', 'withdrawn')
		OR (channel IS NOT NULL AND applied_at IS NOT NULL)
	);

-- One application per user per posting. Without this, a double-click on Save
-- creates two rows, and every count the dashboard shows is quietly wrong.
-- Partial, because a manually-entered application (a company not in our feed)
-- has no posting_id and several of those are legitimate.
CREATE UNIQUE INDEX IF NOT EXISTS applications_user_posting_uniq
	ON applications (user_id, posting_id)
	WHERE posting_id IS NOT NULL;
