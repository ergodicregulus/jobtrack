-- Why did this posting score what it scored?
--
-- Reads the components the scorer STORED, never a recomputation. A recomputed
-- explanation can disagree with the number the user was actually shown, and an
-- explanation that disagrees with the thing it explains is worse than none —
-- which is the same rule the job detail page follows.
--
--   make explain-score EMAIL=grad@jobtrack.local
--   make explain-score EMAIL=grad@jobtrack.local POSTING=26787
\set QUIET on
\pset border 2
\set QUIET off

\echo ''
\echo '── Top matches ──────────────────────────────────────────────'
SELECT p.id,
       left(p.title, 46) AS title,
       c.name            AS company,
       s.score::numeric(4,1),
       s.band,
       s.confidence::numeric(3,2) AS conf,
       s.profile_version AS scorer
  FROM user_job_scores s
  JOIN job_postings p ON p.id = s.posting_id AND p.status = 'live'
  JOIN companies    c ON c.id = p.company_id
  JOIN users        u ON u.id = s.user_id AND u.email = :email
 WHERE (:posting = 0 OR p.id = :posting)
 ORDER BY s.score DESC
 LIMIT CASE WHEN :posting = 0 THEN 10 ELSE 1 END;

\echo ''
\echo '── Component breakdown ──────────────────────────────────────'
\echo '   (neutral = abstained: excluded from BOTH numerator and denominator)'
\echo ''
WITH target AS (
  SELECT s.components, s.posting_id
    FROM user_job_scores s
    JOIN job_postings p ON p.id = s.posting_id AND p.status = 'live'
    JOIN users        u ON u.id = s.user_id AND u.email = :email
   WHERE (:posting = 0 OR p.id = :posting)
   ORDER BY s.score DESC
   LIMIT 1
)
SELECT c->>'name'                                   AS component,
       CASE WHEN (c->>'neutral')::bool THEN '—'
            ELSE (c->>'score')::numeric(5,1)::text END AS earned,
       (c->>'max')::numeric(4,0)                    AS max,
       CASE WHEN (c->>'neutral')::bool THEN 'yes' ELSE '' END AS abstained,
       left(c->>'detail', 62)                       AS why
  FROM target t, jsonb_array_elements(t.components) c;

\echo ''
\echo '── What is missing ──────────────────────────────────────────'
SELECT unnest(COALESCE(s.missing_skills, '{}')) AS missing_skill
  FROM user_job_scores s
  JOIN job_postings p ON p.id = s.posting_id AND p.status = 'live'
  JOIN users        u ON u.id = s.user_id AND u.email = :email
 WHERE (:posting = 0 OR p.id = :posting)
 ORDER BY s.score DESC
 LIMIT 20;
