-- Dismissed postings: the other half of the product's memory.
--
-- Saved searches remember what you want. This remembers what you have already
-- rejected, which is the more common act — a feed you have to re-skim from the
-- top is a feed that punishes you for coming back, and re-reading the same
-- twenty roles you dismissed yesterday is the single loudest complaint about
-- every job board this product is a reaction to.
--
-- WHY THIS IS A TABLE AND NOT A COLUMN ON user_job_scores. ADR-0016 removed the
-- materialised score table precisely because it grew with users x postings. A
-- dismissal grows with users x postings-they-actually-looked-at, which is a
-- different quantity by three orders of magnitude: a heavy user dismisses tens
-- per session, not tens of thousands. It is also *durable user intent* rather
-- than a derived value, so it is exactly the kind of row the ADR says belongs
-- in Postgres — it cannot be recomputed from anything.
CREATE TABLE IF NOT EXISTS dismissed_postings (
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    posting_id bigint NOT NULL REFERENCES job_postings (id) ON DELETE CASCADE,

    dismissed_at timestamptz NOT NULL DEFAULT now(),

    -- Why, when the user says. Nullable and never inferred: a dismissal with a
    -- guessed reason is worse than one with none, because the reason is the
    -- part a future scoring change would learn from.
    --
    -- Deliberately NOT an enum. The set will change as we learn which reasons
    -- people actually give, and ALTER TYPE ... ADD VALUE cannot run in a
    -- transaction — a constraint is the cheaper thing to widen. See
    -- migration 0019 for what the enum route costs.
    reason text CHECK (reason IS NULL OR reason IN
        ('not_interested', 'wrong_location', 'wrong_level', 'wrong_comp', 'already_applied')),

    -- The pair IS the identity: dismissing twice is the same fact, not two.
    -- Primary key rather than a surrogate id plus a unique index, because there
    -- is nothing else to reference a dismissal BY.
    PRIMARY KEY (user_id, posting_id)
);

-- The feed asks one question of this table: "of these postings, which has this
-- user dismissed?" The primary key already leads with user_id and answers it.
--
-- The second index serves the opposite direction — the undo list, newest first
-- — which the primary key cannot, because it orders by posting_id.
CREATE INDEX IF NOT EXISTS dismissed_postings_recent_idx
    ON dismissed_postings (user_id, dismissed_at DESC);
