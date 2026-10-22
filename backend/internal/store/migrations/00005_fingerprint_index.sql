-- Phase 10 (docs/perf.md, "Deleting an import"):
--
-- tx_transfer_peer_idx: transactions.transfer_peer_id references transactions ON DELETE SET NULL,
-- so deleting a transaction makes PostgreSQL look for rows pointing at it. Without an index that
-- is a scan of every transaction per deleted row: deleting a 917-row import took ~27 s in the E2E
-- stack, nearly all of it in the orphan clean-up.
-- import_rows_fp_idx: per-fingerprint lookups of staged rows (re-materialisation after a commit, a
-- delete or a duplicate decision, and the duplicates window) had no index and read every staged
-- row of the user's imports.
-- Both are partial: most rows have no transfer peer, and errored rows have no fingerprint.

-- +goose Up
CREATE INDEX tx_transfer_peer_idx ON transactions (transfer_peer_id) WHERE transfer_peer_id IS NOT NULL;
CREATE INDEX import_rows_fp_idx ON import_rows (user_id, fingerprint) WHERE fingerprint IS NOT NULL;

-- +goose Down
DROP INDEX import_rows_fp_idx;
DROP INDEX tx_transfer_peer_idx;
