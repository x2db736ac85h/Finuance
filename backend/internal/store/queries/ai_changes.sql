-- Changes the assistant made (Phase 20, ADR 0030). User-owned with forced RLS; written in the same
-- transaction as the change by internal/ledger, undone by internal/aichanges.

-- name: OpenAIChange :one
-- The open change of a proposal, created by the first request of its Apply; later requests of the
-- same Apply add their items to it.
INSERT INTO ai_changes (id, user_id, chat_id, proposal_key, action, auto, applied_at)
VALUES (@id, @user_id, @chat_id, @proposal_key, @action, @auto, @now)
ON CONFLICT (user_id, chat_id, proposal_key) WHERE reverted_at IS NULL
DO UPDATE SET auto = ai_changes.auto
RETURNING id;

-- name: UpsertAIChangeItem :exec
-- The first "before" of an entity stays (it is what the change replaced); "after" follows the
-- latest request.
INSERT INTO ai_change_items (change_id, user_id, entity, entity_id, before, after)
VALUES (@change_id, @user_id, @entity, @entity_id, sqlc.narg(before), sqlc.narg(after))
ON CONFLICT (change_id, entity, entity_id) DO UPDATE SET after = EXCLUDED.after;

-- name: ListAIChanges :many
SELECT c.id, c.chat_id, c.proposal_key, c.action, c.auto, c.applied_at, c.reverted_at,
       (SELECT count(*) FROM ai_change_items i WHERE i.change_id = c.id)::int AS items
FROM ai_changes c
WHERE c.user_id = @user_id
  AND (sqlc.narg(before_applied)::timestamptz IS NULL OR (c.applied_at, c.id) < (sqlc.narg(before_applied)::timestamptz, @before_id::uuid))
ORDER BY c.applied_at DESC, c.id DESC
LIMIT @page_size;

-- name: GetAIChangeForUpdate :one
SELECT id, chat_id, proposal_key, action, auto, applied_at, reverted_at
FROM ai_changes WHERE user_id = @user_id AND id = @id
FOR UPDATE;

-- name: ListAIChangeItems :many
SELECT entity, entity_id, before, after FROM ai_change_items
WHERE user_id = @user_id AND change_id = @change_id
ORDER BY entity, entity_id;

-- name: MarkAIChangeReverted :exec
UPDATE ai_changes SET reverted_at = @now WHERE user_id = @user_id AND id = @id;

-- name: TransactionAnnotationState :many
-- What an annotation change replaces: the category (and where it came from), the group and the note.
SELECT id, category_id, category_source, group_id, note FROM transactions
WHERE user_id = @user_id AND id = ANY(@ids::uuid[])
FOR UPDATE;

-- name: CountAIChanges :one
SELECT count(*) FROM ai_changes WHERE user_id = @user_id;

-- name: ExportAIChanges :many
SELECT c.id, c.chat_id, c.proposal_key, c.action, c.auto, c.applied_at, c.reverted_at,
       i.entity, i.entity_id, i.before, i.after
FROM ai_changes c JOIN ai_change_items i ON i.change_id = c.id
WHERE c.user_id = @user_id
ORDER BY c.applied_at, c.id, i.entity, i.entity_id;

-- name: CountGroupUse :one
-- Whether a group the assistant created holds anything (a revert deletes it only when empty).
SELECT ((SELECT count(*) FROM transactions WHERE user_id = @user_id::uuid AND group_id = @id::uuid)
      + (SELECT count(*) FROM tx_annotations WHERE user_id = @user_id::uuid AND group_id = @id::uuid)
      + (SELECT count(*) FROM groups WHERE user_id = @user_id::uuid AND id = @id::uuid AND shared_group_id IS NOT NULL))::bigint;

-- name: CountCategoryUse :one
-- Whether a category the assistant created is used (a revert deletes it only when unused).
SELECT ((SELECT count(*) FROM transactions WHERE user_id = @user_id::uuid AND category_id = @id::uuid)
      + (SELECT count(*) FROM tx_annotations WHERE user_id = @user_id::uuid AND category_id = @id::uuid)
      + (SELECT count(*) FROM rules WHERE user_id = @user_id::uuid AND set_category_id = @id::uuid)
      + (SELECT count(*) FROM budgets WHERE user_id = @user_id::uuid AND category_id = @id::uuid))::bigint;
