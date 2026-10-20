-- name: CreateImport :one
INSERT INTO imports (id, user_id, account_id, filename, size_bytes, sha256, status, encoding, delimiter,
                     headers, column_types, mapping, options, mapping_source, header_signature, stats, created_at)
VALUES (@id, @user_id, sqlc.narg(account_id), @filename, @size_bytes, @sha256, 'previewed', @encoding, @delimiter,
        @headers, @column_types, @mapping, @options, @mapping_source, @header_signature, @stats, @now)
RETURNING *;

-- name: GetImport :one
SELECT * FROM imports WHERE user_id = @user_id AND id = @id;

-- name: ListImports :many
SELECT * FROM imports WHERE user_id = @user_id ORDER BY created_at DESC, id DESC LIMIT @max_rows;

-- name: FindImportBySHA :one
-- Exact duplicate file check (PLAN §4.4): an earlier non-failed import with the same bytes.
SELECT id FROM imports WHERE user_id = @user_id AND sha256 = @sha256 AND status <> 'failed';

-- name: UpdateImportPreview :one
-- PATCH while previewed: a new mapping/options and the re-parse results.
UPDATE imports
SET account_id = sqlc.narg(account_id), encoding = @encoding, delimiter = @delimiter, headers = @headers,
    column_types = @column_types, mapping = @mapping, options = @options, mapping_source = @mapping_source,
    header_signature = @header_signature, stats = @stats
WHERE user_id = @user_id AND id = @id AND status = 'previewed'
RETURNING *;

-- name: SetImportStatus :one
-- Moves an import along previewed → committing → committed | failed. from_status guards the move,
-- so two concurrent commits can't both start.
UPDATE imports
SET status = @to_status, error = sqlc.narg(error), stats = coalesce(sqlc.narg(stats), stats),
    committed_at = CASE WHEN @to_status::text = 'committed' THEN @now::timestamptz ELSE committed_at END
WHERE user_id = @user_id AND id = @id AND status = @from_status
RETURNING *;

-- name: DeleteImport :execrows
-- Cascades to import_files and import_rows. Transactions are cleaned up by DeleteOrphanTransactions.
DELETE FROM imports WHERE user_id = @user_id AND id = @id;
