-- Phase 17 (docs/perf.md, "Phase 17: load test"):
--
-- tx_search_trgm_idx indexed search_text alone, so a search read every user's matches from the
-- index before RLS and the user_id condition narrowed them down: with 53 users of ~20,000
-- transactions, "kawiarnia" read 58,661 index entries to find one user's 1,127 (69 ms of an 85 ms
-- query), and the cost grew with the instance rather than with the user's data. With user_id as
-- the first column the GIN scan intersects the user's entries with the trigrams and reads only
-- that user's matches (4 ms). btree_gin gives uuid a GIN operator class; it ships with PostgreSQL
-- and is a trusted extension, so the database owner may create it.
-- Building the index blocks writes to transactions while it runs: 1 million rows took 2 min 35 s
-- with the default maintenance_work_mem (64 MB) and 24 s with 256 MB, set for this migration only.

-- +goose Up
SET LOCAL maintenance_work_mem = '256MB';
CREATE EXTENSION IF NOT EXISTS btree_gin;
CREATE INDEX tx_user_search_trgm_idx ON transactions USING gin (user_id, search_text gin_trgm_ops);
DROP INDEX tx_search_trgm_idx;

-- +goose Down
CREATE INDEX tx_search_trgm_idx ON transactions USING gin (search_text gin_trgm_ops);
DROP INDEX tx_user_search_trgm_idx;
DROP EXTENSION IF EXISTS btree_gin;
