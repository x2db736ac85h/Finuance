-- User annotations keyed by dedupe identity (PLAN §4.4): they outlive the transaction row, so a
-- label comes back when the same transaction is imported again.

-- name: UpsertAnnotation :exec
INSERT INTO tx_annotations AS a (account_id, fingerprint, occurrence, split_scope, user_id, group_id, category_id, note, note_n, updated_at)
VALUES (@account_id, @fingerprint, @occurrence, @split_scope, @user_id, sqlc.narg(group_id), sqlc.narg(category_id), @note, @note_n, @now)
ON CONFLICT (account_id, fingerprint, occurrence, split_scope) DO UPDATE
SET group_id = excluded.group_id, category_id = excluded.category_id, note = excluded.note,
    note_n = excluded.note_n, updated_at = excluded.updated_at;

-- name: GetAnnotation :one
SELECT * FROM tx_annotations
WHERE user_id = @user_id AND account_id = @account_id AND fingerprint = @fingerprint
  AND occurrence = @occurrence AND split_scope = @split_scope;

-- name: DeleteEmptyAnnotations :execrows
-- An annotation with nothing left (no group, no manual category, no note, no transfer override)
-- is deleted (PLAN §4.4 "explicit clear").
DELETE FROM tx_annotations
WHERE user_id = @user_id AND group_id IS NULL AND category_id IS NULL AND note = '' AND transfer_override IS NULL;
