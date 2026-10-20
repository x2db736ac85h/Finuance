-- Import profiles: a remembered mapping per file layout (PLAN §4.4).

-- name: GetImportProfile :one
SELECT * FROM import_profiles WHERE user_id = @user_id AND header_signature = @header_signature;

-- name: UpsertImportProfile :exec
INSERT INTO import_profiles (user_id, header_signature, name, mapping, options, updated_at)
VALUES (@user_id, @header_signature, @name, @mapping, @options, @now)
ON CONFLICT (user_id, header_signature) DO UPDATE
SET mapping = excluded.mapping, options = excluded.options, updated_at = excluded.updated_at;

-- name: ListImportProfiles :many
SELECT * FROM import_profiles WHERE user_id = @user_id ORDER BY updated_at DESC, header_signature;

-- name: DeleteImportProfile :execrows
DELETE FROM import_profiles WHERE user_id = @user_id AND header_signature = @header_signature;
