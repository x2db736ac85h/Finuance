-- Phase 20 (PR 1): changes the assistant made, so they can be shown, filtered and undone (ADR 0030).
--
-- ai_changes: one row per applied proposal (the chat it came from and the proposal's key). The two
-- requests of one Apply (creating the target group or category, then the change itself) add to the
-- same row while it is not reverted. chat_id is not a foreign key: chats expire after their
-- retention, the record of what the assistant changed stays until the account is deleted.
--
-- ai_change_items: what one change touched, entity by entity, with the fields the action set
-- before and after it. A revert first checks that every item still has its "after" state.
--
-- Both are user-owned (forced RLS, the usual tenant policy) and written in the same database
-- transaction as the change they describe.

-- +goose Up
CREATE TABLE ai_changes (
  id            uuid PRIMARY KEY,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  chat_id       uuid NOT NULL,
  proposal_key  text NOT NULL CHECK (proposal_key ~ '^[0-9a-f]{16}$'),
  action        text NOT NULL CHECK (action ~ '^[a-z_]{1,40}$'),
  auto          boolean NOT NULL DEFAULT false,
  applied_at    timestamptz NOT NULL,
  reverted_at   timestamptz
);
-- one open change per proposal; applying it again after a revert starts a new one
CREATE UNIQUE INDEX ai_changes_open_idx ON ai_changes (user_id, chat_id, proposal_key) WHERE reverted_at IS NULL;
CREATE INDEX ai_changes_user_applied_idx ON ai_changes (user_id, applied_at DESC);
ALTER TABLE ai_changes ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_changes FORCE ROW LEVEL SECURITY;
CREATE POLICY ai_changes_tenant ON ai_changes
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

CREATE TABLE ai_change_items (
  change_id  uuid NOT NULL REFERENCES ai_changes(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  entity     text NOT NULL CHECK (entity IN ('transaction', 'group', 'category', 'rule', 'budget', 'goal', 'shared_expense')),
  entity_id  uuid NOT NULL,
  before     jsonb CHECK (before IS NULL OR jsonb_typeof(before) = 'object'),
  after      jsonb CHECK (after IS NULL OR jsonb_typeof(after) = 'object'),
  PRIMARY KEY (change_id, entity, entity_id)
);
-- "was this changed by the assistant?" for rows, groups, rules, budgets and goals
CREATE INDEX ai_change_items_entity_idx ON ai_change_items (user_id, entity, entity_id);
ALTER TABLE ai_change_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_change_items FORCE ROW LEVEL SECURITY;
CREATE POLICY ai_change_items_tenant ON ai_change_items
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE ai_change_items;
DROP TABLE ai_changes;
