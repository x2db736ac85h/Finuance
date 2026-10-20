-- Staged rows are inserted by store.InsertImportRows (hand-written unnest, see importrows.go).

-- name: ListImportRows :many
-- A page of staged rows in file order; only_errors restricts it to rows with a parse error.
SELECT * FROM import_rows
WHERE user_id = @user_id AND import_id = @import_id AND (NOT @only_errors::bool OR parse_error IS NOT NULL)
ORDER BY row_index
LIMIT @max_rows OFFSET @skip_rows;

-- name: CountImportRows :one
SELECT count(*) AS rows, count(*) FILTER (WHERE parse_error IS NOT NULL) AS errors,
       count(transaction_id) AS linked
FROM import_rows WHERE user_id = @user_id AND import_id = @import_id;

-- name: DeleteImportRows :execrows
-- Re-staging (PATCH / reprocess) and the first step of deleting an import.
DELETE FROM import_rows WHERE user_id = @user_id AND import_id = @import_id;
