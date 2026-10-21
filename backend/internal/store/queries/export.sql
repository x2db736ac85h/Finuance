-- Queries for the personal data export (GET /me/export, PLAN task 7.10).

-- name: ExportAnnotations :many
SELECT * FROM tx_annotations WHERE user_id = @user_id ORDER BY account_id, fingerprint, occurrence, split_scope;

-- name: ExportDedupeOverrides :many
SELECT * FROM dedupe_overrides WHERE user_id = @user_id ORDER BY account_id, fingerprint;

-- name: ExportImportIDs :many
-- Every import of the user, oldest first (the export streams their files one at a time).
SELECT id FROM imports WHERE user_id = @user_id ORDER BY created_at, id;
