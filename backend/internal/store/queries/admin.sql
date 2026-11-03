-- Instance administration (ADR 0025). users, instance_settings and themes are outside RLS and are
-- reached through store.WithoutUser by the auth and admin packages only. The usage counts at the
-- end read user-owned tables and run under each user's own context (store.WithUserReadOnly), one
-- user at a time; they return numbers, never rows.

-- name: AdminListUsers :many
-- One page of accounts, newest first, optionally filtered by part of the e-mail address.
SELECT * FROM users
WHERE (@search::text = '' OR email ILIKE '%' || @search::text || '%')
  AND (sqlc.narg(before_created)::timestamptz IS NULL OR (created_at, id) < (sqlc.narg(before_created)::timestamptz, @before_id::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @page_size;

-- name: AdminCountUsers :one
SELECT count(*) AS total,
       count(*) FILTER (WHERE is_admin AND disabled_at IS NULL) AS admins,
       count(*) FILTER (WHERE disabled_at IS NOT NULL) AS disabled,
       count(*) FILTER (WHERE is_demo) AS demo,
       count(*) FILTER (WHERE last_login_at > @active7::timestamptz) AS active7,
       count(*) FILTER (WHERE last_login_at > @active30::timestamptz) AS active30
FROM users;

-- name: CountActiveAdmins :one
-- Locks the administrators' rows, so two requests can't demote the last two at the same time.
SELECT count(*) FROM (SELECT id FROM users WHERE is_admin AND disabled_at IS NULL FOR UPDATE) a;

-- name: CreateUserWithRole :one
INSERT INTO users (id, email, password_hash, display_name, is_admin, must_change_password)
VALUES (@id, @email, @password_hash, @display_name, @is_admin, @must_change_password)
RETURNING *;

-- name: SetUserAdmin :execrows
UPDATE users SET is_admin = @is_admin WHERE id = @id;

-- name: SetUserDisabled :execrows
UPDATE users SET disabled_at = sqlc.narg(disabled_at) WHERE id = @id;

-- name: SetUserMustChangePassword :exec
UPDATE users SET must_change_password = @must_change WHERE id = @id;

-- name: SetUserPasswordMustChange :exec
-- An administrator's reset: a new one-time password the user has to replace at the next sign-in.
UPDATE users SET password_hash = @password_hash, must_change_password = true WHERE id = @id;

-- name: SetUserLimits :execrows
UPDATE users SET limits = @limits WHERE id = @id;

-- name: EnsureInstanceSettings :exec
-- The migration seeds the row; this restores it if it was ever removed (tests truncate tables).
INSERT INTO instance_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- name: GetInstanceSettings :one
SELECT * FROM instance_settings WHERE id = 1;

-- name: PutInstanceSettings :execrows
-- Versioned: only the document the administrator read can be replaced.
UPDATE instance_settings
SET doc = @doc, version = version + 1, updated_at = @now, updated_by = @updated_by
WHERE id = 1 AND version = @version;

-- name: ListThemes :many
SELECT * FROM themes ORDER BY name, id;

-- name: ListApprovedThemes :many
SELECT * FROM themes WHERE status = 'approved' ORDER BY name, id;

-- name: GetTheme :one
SELECT * FROM themes WHERE id = @id;

-- name: CreateTheme :one
INSERT INTO themes (id, slug, name, based_on, tokens, created_by, created_at, updated_at)
VALUES (@id, @slug, @name, @based_on, @tokens, @created_by, @now, @now)
RETURNING *;

-- name: UpdateTheme :one
-- Versioned like the settings; status changes go through SetThemeStatus.
UPDATE themes SET name = @name, tokens = @tokens, version = version + 1, updated_at = @now
WHERE id = @id AND version = @version
RETURNING *;

-- name: SetThemeStatus :one
UPDATE themes SET status = @status, version = version + 1, updated_at = @now
WHERE id = @id AND version = @version
RETURNING *;

-- name: DeleteDraftTheme :execrows
DELETE FROM themes WHERE id = @id AND status = 'draft';

-- name: UserUsage :one
-- Run under the user's own RLS context: counts only.
SELECT
  (SELECT count(*) FROM accounts a WHERE a.user_id = @user_id) AS accounts,
  (SELECT count(*) FROM transactions t WHERE t.user_id = @user_id) AS transactions,
  (SELECT count(*) FROM imports i WHERE i.user_id = @user_id AND i.status = 'committed') AS imports,
  (SELECT coalesce(sum(i.size_bytes), 0)::bigint FROM imports i WHERE i.user_id = @user_id) AS stored_bytes,
  (SELECT count(*) FROM assistant_chats c WHERE c.user_id = @user_id) AS chats,
  -- the epoch stands for "no activity recorded" (pgx can't scan NULL into a time)
  coalesce((SELECT max(e.at) FROM audit_log e WHERE e.user_id = @user_id), 'epoch'::timestamptz)::timestamptz AS last_activity;

-- name: UserActivityByDay :many
-- Imports and assistant questions per day since a date, for the instance usage page.
SELECT d.day::date AS day,
       (SELECT count(*) FROM imports i WHERE i.user_id = @user_id AND i.created_at >= d.day AND i.created_at < d.day + interval '1 day')::bigint AS imports,
       (SELECT count(*) FROM assistant_messages m WHERE m.user_id = @user_id AND m.role = 'user'
          AND m.created_at >= d.day AND m.created_at < d.day + interval '1 day')::bigint AS questions
FROM generate_series(@since::date, @until::date, interval '1 day') AS d(day);

-- name: CountImportsSince :one
-- For the imports-per-day limit (run under the user's context).
SELECT count(*) FROM imports WHERE user_id = @user_id AND created_at >= @since;

-- name: CountAccounts :one
SELECT count(*) FROM accounts WHERE user_id = @user_id;

-- name: CountGroups :one
SELECT count(*) FROM groups WHERE user_id = @user_id;
