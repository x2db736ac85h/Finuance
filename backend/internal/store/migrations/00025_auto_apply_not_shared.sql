-- Phase 22 (22.12, AI-1): sharing with a shared group and changing a shared split are no longer
-- kinds of proposals the panel may apply by itself; other people read the result, so they always
-- wait for a click (ADR 0030 note of 2026-11-19). Stored choices lose the two kinds, and the check
-- refuses them from now on.

-- +goose Up
UPDATE assistant_settings
SET auto_apply = array_remove(array_remove(auto_apply, 'add_to_shared_group'), 'split_expense')
WHERE auto_apply && ARRAY['add_to_shared_group', 'split_expense']::text[];
ALTER TABLE assistant_settings DROP CONSTRAINT assistant_settings_auto_apply_check;
ALTER TABLE assistant_settings ADD CONSTRAINT assistant_settings_auto_apply_check
  CHECK (auto_apply <@ ARRAY['set_category', 'set_group', 'create_rule', 'set_budget', 'add_expense',
                             'create_goal', 'update_goal']::text[]);

-- +goose Down
ALTER TABLE assistant_settings DROP CONSTRAINT assistant_settings_auto_apply_check;
ALTER TABLE assistant_settings ADD CONSTRAINT assistant_settings_auto_apply_check
  CHECK (auto_apply <@ ARRAY['set_category', 'set_group', 'create_rule', 'set_budget', 'add_expense',
                             'add_to_shared_group', 'split_expense', 'create_goal', 'update_goal']::text[]);
