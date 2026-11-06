-- Phase 20 (PR 1): the kinds of assistant proposals a user lets the panel apply by itself (ADR 0030).
-- Empty by default; only these action names. The panel never applies a digest's proposals or one
-- touching more than 50 transactions by itself, whatever is listed here.

-- +goose Up
ALTER TABLE assistant_settings
  ADD COLUMN auto_apply text[] NOT NULL DEFAULT '{}'
  CHECK (auto_apply <@ ARRAY['set_category', 'set_group', 'create_rule', 'set_budget', 'add_expense',
                             'add_to_shared_group', 'split_expense', 'create_goal', 'update_goal']::text[]);

-- +goose Down
ALTER TABLE assistant_settings DROP COLUMN auto_apply;
