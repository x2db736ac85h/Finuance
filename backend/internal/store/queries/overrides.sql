-- Duplicate review overrides (PLAN §4.4, legacy §32.3). Re-materialisation comes in Phase 6.

-- name: SetDedupeOverride :one
INSERT INTO dedupe_overrides AS d (account_id, fingerprint, user_id, mode, preferred_import_id, updated_at)
VALUES (@account_id, @fingerprint, @user_id, @mode, sqlc.narg(preferred_import_id), @now)
ON CONFLICT (account_id, fingerprint) DO UPDATE
SET mode = excluded.mode, preferred_import_id = excluded.preferred_import_id, updated_at = excluded.updated_at
RETURNING *;

-- name: ListDedupeOverrides :many
SELECT * FROM dedupe_overrides WHERE user_id = @user_id AND account_id = @account_id ORDER BY fingerprint;

-- name: ClearDedupeOverride :execrows
DELETE FROM dedupe_overrides WHERE user_id = @user_id AND account_id = @account_id AND fingerprint = @fingerprint;
