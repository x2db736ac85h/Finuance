-- Phase 20 (PR 1): goals in plain language (ADR 0031). Two kinds:
--
-- save:   put target_minor aside by the deadline; measured as income minus spending of every
--         account since starts_on (transfers left out), or as the growth of one account.
-- reduce: spend reduce_pct % less on a category than the average of the baseline_months full
--         months before starts_on, every month until the deadline.
--
-- Category limits stay budgets. User-owned (forced RLS, the usual tenant policy). A deleted
-- category or account leaves the goal without one (the app shows it needs attention); merging a
-- category moves its goals along.

-- +goose Up
CREATE TABLE goals (
  id               uuid PRIMARY KEY,
  user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
  kind             text NOT NULL CHECK (kind IN ('save', 'reduce')),
  target_minor     bigint CHECK (target_minor > 0),
  currency         char(3) NOT NULL,
  account_id       uuid REFERENCES accounts(id) ON DELETE SET NULL,
  category_id      uuid REFERENCES categories(id) ON DELETE SET NULL,
  reduce_pct       smallint CHECK (reduce_pct BETWEEN 1 AND 100),
  baseline_months  smallint NOT NULL DEFAULT 3 CHECK (baseline_months BETWEEN 1 AND 12),
  starts_on        date NOT NULL,
  deadline         date NOT NULL,
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'done', 'archived')),
  version          integer NOT NULL DEFAULT 1,
  created_at       timestamptz NOT NULL,
  updated_at       timestamptz NOT NULL,
  CHECK (deadline > starts_on),
  CHECK (kind <> 'save' OR target_minor IS NOT NULL),
  CHECK (kind <> 'reduce' OR reduce_pct IS NOT NULL)
);
CREATE INDEX goals_user_idx ON goals (user_id, deadline);
CREATE INDEX goals_account_idx ON goals (account_id) WHERE account_id IS NOT NULL;
CREATE INDEX goals_category_idx ON goals (category_id) WHERE category_id IS NOT NULL;
ALTER TABLE goals ENABLE ROW LEVEL SECURITY;
ALTER TABLE goals FORCE ROW LEVEL SECURITY;
CREATE POLICY goals_tenant ON goals
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE goals;
