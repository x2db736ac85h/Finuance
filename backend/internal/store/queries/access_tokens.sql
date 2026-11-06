-- Personal access tokens (ADR 0036; migration 00023). Outside RLS like sessions; every query but
-- the lookup by hash filters by user_id.

-- name: InsertAccessToken :one
INSERT INTO access_tokens (id, user_id, name, token_hash, prefix, scope, created_at, expires_at)
VALUES (@id, @user_id, @name, @token_hash, @prefix, @scope, @now, @expires_at)
RETURNING *;

-- name: ResolveAccessToken :one
-- A live token and its user: not revoked, not expired, and the account not disabled.
SELECT t.id AS token_id, t.scope, t.last_used_at, t.user_id,
       u.email, u.display_name, u.is_demo, u.created_at AS user_created_at,
       u.is_admin, u.must_change_password, u.limits
FROM access_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = @token_hash AND t.revoked_at IS NULL AND t.expires_at > @now AND u.disabled_at IS NULL;

-- name: TouchAccessToken :exec
-- Records use; the caller throttles it to once a minute.
UPDATE access_tokens SET last_used_at = @now::timestamptz WHERE id = @id;

-- name: ListAccessTokens :many
SELECT * FROM access_tokens WHERE user_id = @user_id ORDER BY created_at DESC, id;

-- name: CountLiveAccessTokens :one
SELECT count(*) FROM access_tokens WHERE user_id = @user_id AND revoked_at IS NULL AND expires_at > @now;

-- name: RevokeAccessToken :one
UPDATE access_tokens SET revoked_at = @now::timestamptz WHERE user_id = @user_id AND id = @id AND revoked_at IS NULL
RETURNING *;

-- name: RevokeUserAccessTokens :execrows
-- A new password, or an administrator signing the account out everywhere, ends its tokens too.
UPDATE access_tokens SET revoked_at = @now::timestamptz WHERE user_id = @user_id AND revoked_at IS NULL;

-- name: DeleteOldAccessTokens :execrows
-- Expired or revoked more than 30 days ago (the hourly session cleanup).
DELETE FROM access_tokens WHERE (revoked_at IS NOT NULL AND revoked_at < @cutoff::timestamptz) OR expires_at < @cutoff::timestamptz;
