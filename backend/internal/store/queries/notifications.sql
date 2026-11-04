-- In-app notifications (Phase 19, 19.8). User-owned with forced RLS; another user's action writes
-- a notification under the recipient's own context (internal/notify).

-- name: InsertNotification :exec
INSERT INTO notifications (id, user_id, kind, data, created_at)
VALUES (@id, @user_id, @kind, @data, @now);

-- name: ListNotifications :many
SELECT id, kind, data, created_at, read_at FROM notifications
WHERE user_id = @user_id
ORDER BY created_at DESC, id DESC
LIMIT @page_size;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications WHERE user_id = @user_id AND read_at IS NULL;

-- name: MarkNotificationsRead :execrows
-- Marks the given notifications read, or every unread one when ids is NULL.
UPDATE notifications SET read_at = @now
WHERE user_id = @user_id AND read_at IS NULL
  AND (sqlc.narg(ids)::uuid[] IS NULL OR id = ANY(sqlc.narg(ids)::uuid[]));

-- name: DeleteOldNotifications :execrows
DELETE FROM notifications WHERE user_id = @user_id AND created_at < @before;
