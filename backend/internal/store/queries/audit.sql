-- The audit log (PLAN task 17.6, migration 00010). Rows are only ever inserted, listed and, after
-- the retention, deleted (the table's policies allow nothing else).

-- name: InsertAuditEvent :exec
INSERT INTO audit_log (user_id, action, entity_id, ip, browser, meta)
VALUES (@user_id, @action, sqlc.narg(entity_id), sqlc.narg(ip), @browser, @meta);

-- name: ListAuditEvents :many
-- Newest first, a page at a time: rows with an id below before_id (0 = from the newest).
SELECT id, at, action, entity_id, ip, browser, meta FROM audit_log
WHERE user_id = @user_id AND (@before_id::bigint = 0 OR id < @before_id::bigint)
ORDER BY id DESC
LIMIT @page_size;

-- name: AllAuditEvents :many
-- The data export: every row of the user, oldest first.
SELECT id, at, action, entity_id, ip, browser, meta FROM audit_log WHERE user_id = @user_id ORDER BY id;

-- name: DeleteExpiredAuditEvents :execrows
-- The daily cleanup; the delete policy allows only rows past the retention anyway.
DELETE FROM audit_log WHERE user_id = @user_id AND at < now() - interval '365 days';

-- name: AppliedProposals :many
-- The proposal keys of one chat that the user applied (the panel's "Applied" state).
SELECT DISTINCT (meta->>'proposal')::text AS proposal_key
FROM audit_log
WHERE user_id = $1 AND action = 'assistant.action_applied' AND entity_id = sqlc.arg(chat_id)::text
  AND meta ? 'proposal'
ORDER BY 1;
