-- The schema as a stable, sorted, line-per-fact listing.
--
-- Introspection rather than a pg_dump diff, which was tried first and abandoned:
-- pg_dump emits multi-line statements, so filtering unwanted objects line by
-- line leaves orphaned fragments, and pg_dump 17 stamps each run with a random
-- \restrict token that makes two dumps of the SAME database differ. This asks
-- the catalogue directly and gets one comparable line per fact.
--
-- River's own objects are excluded throughout: River creates and migrates its
-- own tables at startup by design (ADR-0005), so they are absent from
-- migrations/ on purpose. Reporting that as drift every single run is how a
-- drift check gets ignored and then deleted.
\pset tuples_only on
\pset format unaligned
\pset footer off

SELECT 'column ' || table_name || '.' || column_name || ' ' || data_type
       || ' null=' || is_nullable
       || ' default=' || COALESCE(regexp_replace(column_default, '::[a-z_ ]+', '', 'g'), '-')
  FROM information_schema.columns
 WHERE table_schema = 'public'
   AND table_name NOT LIKE 'river%'
   AND table_name <> 'schema_migrations'
 ORDER BY 1;

SELECT 'constraint ' || rel.relname || '.' || con.conname || ' ' || pg_get_constraintdef(con.oid)
  FROM pg_constraint con
  JOIN pg_class rel ON rel.oid = con.conrelid
  JOIN pg_namespace ns ON ns.oid = rel.relnamespace
 WHERE ns.nspname = 'public'
   AND rel.relname NOT LIKE 'river%'
   AND rel.relname <> 'schema_migrations'
 ORDER BY 1;

SELECT 'index ' || indexname || ' ' || regexp_replace(indexdef, '^CREATE (UNIQUE )?INDEX [^ ]+ ', '')
  FROM pg_indexes
 WHERE schemaname = 'public'
   AND tablename NOT LIKE 'river%'
   AND tablename <> 'schema_migrations'
 ORDER BY 1;

SELECT 'enum ' || t.typname || ' = ' || string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder)
  FROM pg_type t
  JOIN pg_enum e ON e.enumtypid = t.oid
  JOIN pg_namespace ns ON ns.oid = t.typnamespace
 WHERE ns.nspname = 'public'
   AND t.typname NOT LIKE 'river%'
 GROUP BY t.typname
 ORDER BY 1;
