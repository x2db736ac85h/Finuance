-- Staged rows are inserted by store.InsertImportRows (hand-written unnest, see importrows.go).

-- name: ListImportRows :many
-- A page of staged rows in file order; only_errors restricts it to rows with a parse error.
-- duplicate: for a committed row, its transaction takes its values from another import (the row
-- was merged); before the commit, the import's account already has a transaction with this
-- (fingerprint, occurrence) — the preview's per-row estimate.
SELECT ir.*,
       CASE WHEN ir.parse_error IS NOT NULL THEN false
            WHEN ir.transaction_id IS NOT NULL THEN
              coalesce((SELECT t.source_import_id IS DISTINCT FROM ir.import_id FROM transactions t WHERE t.id = ir.transaction_id), false)
            ELSE EXISTS (SELECT 1 FROM transactions t
                         WHERE t.user_id = ir.user_id AND t.account_id = i.account_id
                           AND t.fingerprint = ir.fingerprint AND t.occurrence = ir.occurrence)
       END::bool AS duplicate
FROM import_rows ir
JOIN imports i ON i.id = ir.import_id
WHERE ir.user_id = @user_id AND ir.import_id = @import_id AND (NOT @only_errors::bool OR ir.parse_error IS NOT NULL)
ORDER BY ir.row_index
LIMIT @max_rows OFFSET @skip_rows;

-- name: CountDuplicateEstimate :one
-- Valid staged rows whose (fingerprint, occurrence) the account already has: the preview's
-- "duplicates" estimate before the commit.
SELECT count(*) FROM import_rows ir
WHERE ir.user_id = @user_id AND ir.import_id = @import_id AND ir.parse_error IS NULL
  AND EXISTS (SELECT 1 FROM transactions t WHERE t.user_id = @user_id AND t.account_id = @account_id
              AND t.fingerprint = ir.fingerprint AND t.occurrence = ir.occurrence);

-- name: CountImportRows :one
SELECT count(*) AS rows, count(*) FILTER (WHERE parse_error IS NOT NULL) AS errors,
       count(transaction_id) AS linked
FROM import_rows WHERE user_id = @user_id AND import_id = @import_id;

-- name: DeleteImportRows :execrows
-- Re-staging (PATCH / reprocess) and the first step of deleting an import.
DELETE FROM import_rows WHERE user_id = @user_id AND import_id = @import_id;
