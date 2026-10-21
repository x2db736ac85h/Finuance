-- Phase 7: indexes for the transaction query engine (PLAN task 7.11, docs/perf.md).
--
-- tx_user_booked_idx now covers what the default order, the window id lookup and the totals
-- (count, sum/avg/min/max of the amount and balance, the date range) read, so those run as
-- index-only scans for unfiltered and period-only contexts.
-- import_rows_linked_idx serves "N duplicates skipped" (linked staging rows per import) without
-- reading the wide import_rows heap. user_id is in both indexes because the RLS policies read it.

-- +goose Up
DROP INDEX tx_user_booked_idx;
CREATE INDEX tx_user_booked_idx ON transactions (user_id, booked_on DESC, seq)
  INCLUDE (id, account_id, amount_minor, balance_after_minor, is_transfer);
CREATE INDEX import_rows_linked_idx ON import_rows (user_id, import_id) INCLUDE (transaction_id)
  WHERE transaction_id IS NOT NULL;

-- +goose Down
DROP INDEX import_rows_linked_idx;
DROP INDEX tx_user_booked_idx;
CREATE INDEX tx_user_booked_idx ON transactions (user_id, booked_on DESC, seq);
