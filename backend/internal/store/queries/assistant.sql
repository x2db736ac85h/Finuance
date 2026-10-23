-- The assistant's settings, chats and messages (PLAN §4.8; migration 00006).

-- name: GetAssistantSettings :one
SELECT * FROM assistant_settings WHERE user_id = @user_id;

-- name: UpsertAssistantSettings :one
-- retention_days NULL keeps chats until the user deletes them.
INSERT INTO assistant_settings (user_id, retention_days, cloud_consent, updated_at)
VALUES (@user_id, sqlc.narg(retention_days), @cloud_consent, @now)
ON CONFLICT (user_id) DO UPDATE
SET retention_days = excluded.retention_days, cloud_consent = excluded.cloud_consent, updated_at = excluded.updated_at
RETURNING *;

-- name: CreateAssistantChat :one
INSERT INTO assistant_chats (id, user_id, title, connection, model, created_at, updated_at)
VALUES (@id, @user_id, @title, @connection, @model, @now, @now)
RETURNING *;

-- name: GetAssistantChat :one
SELECT * FROM assistant_chats WHERE user_id = @user_id AND id = @id;

-- name: ListAssistantChats :many
SELECT * FROM assistant_chats WHERE user_id = @user_id ORDER BY updated_at DESC, id;

-- name: UpdateAssistantChat :exec
-- After a turn: the last activity, the connection and model used, and the title (the first
-- question) when the chat had none.
UPDATE assistant_chats
SET updated_at = @now, connection = @connection, model = @model, no_tools = @no_tools,
    title = CASE WHEN title = '' THEN @title ELSE title END
WHERE user_id = @user_id AND id = @id;

-- name: DeleteAssistantChat :execrows
DELETE FROM assistant_chats WHERE user_id = @user_id AND id = @id;

-- name: DeleteAssistantChatsBefore :execrows
-- Retention: chats whose last message is older than the cutoff.
DELETE FROM assistant_chats WHERE user_id = @user_id AND updated_at < @cutoff;

-- name: TrimAssistantChats :execrows
-- Keeps the newest @keep chats (FINUANCE_AI_MAX_CHATS).
DELETE FROM assistant_chats d
WHERE d.user_id = @user_id AND d.id NOT IN (
  SELECT c.id FROM assistant_chats c WHERE c.user_id = @user_id ORDER BY c.updated_at DESC, c.id LIMIT @keep);

-- name: InsertAssistantMessage :one
INSERT INTO assistant_messages (chat_id, user_id, seq, role, content, reasoning, tool_calls, tool_call_id, replay, steps, meta, error, created_at)
VALUES (@chat_id, @user_id,
        (SELECT coalesce(max(m.seq), 0) + 1 FROM assistant_messages m WHERE m.chat_id = @chat_id),
        @role, @content, @reasoning, @tool_calls, @tool_call_id, @replay, @steps, @meta, @error, @now)
RETURNING id, seq;

-- name: ListAssistantMessages :many
SELECT * FROM assistant_messages WHERE user_id = @user_id AND chat_id = @chat_id ORDER BY seq;

-- name: ListUserIDsAfter :many
-- Users in id order, a page at a time (the retention job visits every user under its own RLS
-- context; users is not under RLS).
SELECT id FROM users WHERE id > @after ORDER BY id LIMIT @page_size;
