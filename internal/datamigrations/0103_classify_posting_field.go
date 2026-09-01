package datamigrations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jobtrack/jobtrack/internal/normalise"
)

// ClassifyPostingField labels every stored posting with the kind of work it is.
//
// New postings are classified at ingest. These are the ones already stored, and
// waiting for a re-poll would not fix them: a posting that has dropped off its
// board is never upserted again, and until then the feed's default filter would
// treat every unclassified row as 'unknown' — which is the safe default, but it
// means the filter does nothing for most of the corpus on the day it ships.
//
// IN GO, NOT IN SQL, and that is the point of it being a data migration at all.
// The classifier is a table of tokens with an ordering that matters, and it has
// to give the same answer here as it does at ingest. Two implementations of one
// judgement is the drift this repository keeps finding; running the real
// function over stored rows is the only way to be sure they agree.
type ClassifyPostingField struct{}

const classifyBatchSize = 2000

func (m *ClassifyPostingField) Version() int64 { return 103 }
func (m *ClassifyPostingField) Name() string   { return "classify_posting_field" }

// RequiresSchemaVersion is 24: the field columns arrive there.
func (m *ClassifyPostingField) RequiresSchemaVersion() int64 { return 24 }

func (m *ClassifyPostingField) EstimateTotal(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM job_postings WHERE field_because = ''`).Scan(&n)
	return n, err
}

// row is one posting's classification inputs. The skill count comes from
// posting_skills rather than being recomputed, because that is what the ingest
// path uses and the two must not diverge.
type row struct {
	ID     int64  `db:"id"`
	Title  string `db:"title"`
	Skills int    `db:"skills"`
}

func (m *ClassifyPostingField) Batch(
	ctx context.Context, pool *pgxpool.Pool, cursor []byte,
) (next []byte, rows int64, done bool, err error) {
	after := decodeCursor(cursor)

	found, err := pool.Query(ctx, `
		SELECT p.id, p.title,
		       (SELECT count(*) FROM posting_skills ps WHERE ps.posting_id = p.id)::int AS skills
		  FROM job_postings p
		 WHERE p.id > $1
		 ORDER BY p.id
		 LIMIT $2`, after, classifyBatchSize)
	if err != nil {
		return cursor, 0, false, fmt.Errorf("read batch after id %d: %w", after, err)
	}
	batch, err := pgx.CollectRows(found, pgx.RowToStructByName[row])
	if err != nil {
		return cursor, 0, false, fmt.Errorf("collect batch after id %d: %w", after, err)
	}
	if len(batch) == 0 {
		return cursor, 0, true, nil
	}

	ids := make([]int64, len(batch))
	fields := make([]string, len(batch))
	confs := make([]float64, len(batch))
	becauses := make([]string, len(batch))
	for i, r := range batch {
		g := normalise.ClassifyField(r.Title, r.Skills)
		ids[i], fields[i], confs[i], becauses[i] = r.ID, string(g.Field), g.Confidence, g.Because
	}

	tag, err := pool.Exec(ctx, `
		UPDATE job_postings p
		   SET field = x.field, field_confidence = x.conf, field_because = x.because,
		       updated_at = now()
		  FROM unnest($1::bigint[], $2::text[], $3::float8[], $4::text[])
		       AS x(id, field, conf, because)
		 WHERE p.id = x.id`, ids, fields, confs, becauses)
	if err != nil {
		return cursor, 0, false, fmt.Errorf("write batch after id %d: %w", after, err)
	}

	// Advance by the window scanned, not by rows changed: a batch that is
	// already correctly classified changes nothing and would stall a cursor
	// that tracked updates.
	return encodeCursor(batch[len(batch)-1].ID), tag.RowsAffected(), false, nil
}
