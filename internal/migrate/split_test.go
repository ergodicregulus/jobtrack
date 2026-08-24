package migrate

import (
	"strings"
	"testing"
)

// The splitter exists because Postgres wraps multi-statement query strings in an
// implicit transaction, which breaks CREATE INDEX CONCURRENTLY. Every case below
// is a way a naive `strings.Split(sql, ";")` would produce broken SQL — and
// broken SQL in a non-transactional migration means a half-applied schema.
func TestSplitStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "simple statements",
			sql:  "CREATE TABLE a (id int);\nCREATE TABLE b (id int);",
			want: []string{"CREATE TABLE a (id int);", "CREATE TABLE b (id int);"},
		},
		{
			name: "trailing statement without semicolon",
			sql:  "SELECT 1;\nSELECT 2",
			want: []string{"SELECT 1;", "SELECT 2"},
		},
		{
			name: "semicolon inside a string literal must not split",
			sql:  "INSERT INTO t VALUES ('a;b');\nSELECT 1;",
			want: []string{"INSERT INTO t VALUES ('a;b');", "SELECT 1;"},
		},
		{
			name: "escaped quote inside a string literal",
			sql:  "INSERT INTO t VALUES ('it''s; fine');\nSELECT 1;",
			want: []string{"INSERT INTO t VALUES ('it''s; fine');", "SELECT 1;"},
		},
		{
			name: "semicolon inside a quoted identifier",
			sql:  `CREATE TABLE "weird;name" (id int);` + "\nSELECT 1;",
			want: []string{`CREATE TABLE "weird;name" (id int);`, "SELECT 1;"},
		},
		{
			name: "dollar-quoted DO block containing semicolons",
			// This is the real case from 0004: a DO block whose body has
			// semicolons. Splitting naively produces a syntax error.
			sql: "DO $$ BEGIN CREATE TYPE t AS ENUM ('a'); EXCEPTION WHEN duplicate_object THEN NULL; END $$;\nSELECT 1;",
			want: []string{
				"DO $$ BEGIN CREATE TYPE t AS ENUM ('a'); EXCEPTION WHEN duplicate_object THEN NULL; END $$;",
				"SELECT 1;",
			},
		},
		{
			name: "tagged dollar quoting",
			sql:  "CREATE FUNCTION f() RETURNS int AS $body$ BEGIN RETURN 1; END $body$ LANGUAGE plpgsql;\nSELECT 1;",
			want: []string{
				"CREATE FUNCTION f() RETURNS int AS $body$ BEGIN RETURN 1; END $body$ LANGUAGE plpgsql;",
				"SELECT 1;",
			},
		},
		{
			name: "semicolon inside a line comment",
			sql:  "-- a comment; with a semicolon\nSELECT 1;",
			want: []string{"-- a comment; with a semicolon\nSELECT 1;"},
		},
		{
			name: "semicolon inside a block comment",
			sql:  "/* block; comment */ SELECT 1;\nSELECT 2;",
			want: []string{"/* block; comment */ SELECT 1;", "SELECT 2;"},
		},
		{
			name: "blank input produces no statements",
			sql:  "\n\n   \n",
			want: nil,
		},
		{
			name: "comment-only input is preserved as one chunk",
			sql:  "-- just a note\n",
			want: []string{"-- just a note"},
		},
		{
			name: "consecutive semicolons do not produce empty statements",
			sql:  "SELECT 1;;\nSELECT 2;",
			want: []string{"SELECT 1;", ";", "SELECT 2;"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitStatements(tc.sql)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d statements, want %d\ngot:  %#v\nwant: %#v",
					len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if strings.TrimSpace(got[i]) != strings.TrimSpace(tc.want[i]) {
					t.Errorf("statement %d:\n got: %q\nwant: %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// The real migration files must survive splitting. This guards against a future
// migration using syntax the splitter mishandles — it would otherwise only be
// discovered during a deploy.
func TestSplitStatements_RealMigrations(t *testing.T) {
	// 0007 is the only non-transactional migration, so it is the only one that
	// actually goes through the splitter in production.
	const concurrent = `-- +migrate no-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS a_idx ON t USING hnsw (e vector_cosine_ops) WITH (m = 16);
CREATE INDEX CONCURRENTLY IF NOT EXISTS b_idx ON u USING gin (n gin_trgm_ops);`

	got := splitStatements(concurrent)
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(got), got)
	}
	for i, stmt := range got {
		if !strings.Contains(stmt, "CREATE INDEX CONCURRENTLY") {
			t.Errorf("statement %d lost its CREATE INDEX CONCURRENTLY: %q", i, stmt)
		}
		// Each statement must be individually executable — no stray leading
		// semicolon, which would be a syntax error on its own.
		if strings.HasPrefix(strings.TrimSpace(stmt), ";") {
			t.Errorf("statement %d starts with a semicolon: %q", i, stmt)
		}
	}
}

func TestDollarTag(t *testing.T) {
	tests := []struct {
		in      string
		wantTag string
		wantOK  bool
	}{
		{"$$", "$$", true},
		{"$body$", "$body$", true},
		{"$_x1$", "$_x1$", true},
		{"$1", "", false},      // positional parameter, not a dollar quote
		{"$1x$", "", false},    // tags cannot start with a digit
		{"$ab", "", false},     // unterminated
		{"$ab-cd$", "", false}, // hyphen is not an identifier character
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			gotTag, gotOK := dollarTag([]rune(tc.in), 0)
			if gotOK != tc.wantOK || gotTag != tc.wantTag {
				t.Errorf("dollarTag(%q) = (%q, %v), want (%q, %v)",
					tc.in, gotTag, gotOK, tc.wantTag, tc.wantOK)
			}
		})
	}
}
