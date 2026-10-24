-- Phase 15: category management, rules and transfers (PLAN tasks 15.1–15.3).
--
-- category_aliases: which category a bank category name now belongs to. A category is found by
-- its own normalised name; renaming or merging one leaves an alias for the old name, and deleting
-- one leaves an alias to NULL (the bank name stays uncategorised), so re-imports keep the user's
-- changes. An alias wins over a category of the same name; creating or renaming a category to a
-- name removes that name's alias.
--
-- rules: a rule matches with the table's filter language (match = {search, filters}) and sets a
-- category and/or marks transfers. Rules apply in position order; the last match wins (ADR 0017).
--
-- transactions.category_rule_id: the rule that set the category (category_source = 'rule').
-- transactions.transfer_source: why is_transfer is set: a detected pair, a rule, or the user
-- (tx_annotations.transfer_override, which survives re-imports). 'manual' with is_transfer false
-- means "not a transfer": detection and rules leave it alone (ADR 0018).

-- +goose Up
CREATE TABLE category_aliases (
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name_n       text NOT NULL,
  category_id  uuid REFERENCES categories(id) ON DELETE SET NULL,
  PRIMARY KEY (user_id, name_n)
);
CREATE INDEX category_aliases_category_idx ON category_aliases (category_id);

-- The category of each bank category name: its alias, else the category of that name. Every
-- import, re-materialisation and "use the bank's category" resolves through it. security_invoker:
-- the caller's row-level security applies to both tables.
CREATE VIEW bank_category_map WITH (security_invoker = true) AS
  SELECT a.user_id, a.name_n, a.category_id FROM category_aliases a
  UNION ALL
  SELECT c.user_id, c.name_n, c.id AS category_id FROM categories c
  WHERE NOT EXISTS (SELECT 1 FROM category_aliases a WHERE a.user_id = c.user_id AND a.name_n = c.name_n);

CREATE TABLE rules (
  id               uuid PRIMARY KEY,
  user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name             text NOT NULL,
  enabled          boolean NOT NULL DEFAULT true,
  position         integer NOT NULL,
  match            jsonb NOT NULL,
  set_category_id  uuid REFERENCES categories(id) ON DELETE SET NULL,
  mark_transfer    boolean NOT NULL DEFAULT false,
  created_at       timestamptz NOT NULL,
  updated_at       timestamptz NOT NULL
);
CREATE INDEX rules_user_position_idx ON rules (user_id, position);
CREATE INDEX rules_category_idx ON rules (set_category_id);

ALTER TABLE transactions
  ADD COLUMN category_rule_id uuid REFERENCES rules(id) ON DELETE SET NULL,
  ADD COLUMN transfer_source text NOT NULL DEFAULT 'none' CHECK (transfer_source IN ('none', 'pair', 'rule', 'manual'));
CREATE INDEX tx_category_rule_idx ON transactions (category_rule_id);
-- transfer detection looks for opposite amounts among a user's unmarked transactions
CREATE INDEX tx_user_amount_idx ON transactions (user_id, amount_minor) WHERE transfer_source = 'none';

ALTER TABLE category_aliases ENABLE ROW LEVEL SECURITY;
ALTER TABLE category_aliases FORCE ROW LEVEL SECURITY;
CREATE POLICY category_aliases_tenant ON category_aliases
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE rules FORCE ROW LEVEL SECURITY;
CREATE POLICY rules_tenant ON rules
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP INDEX tx_user_amount_idx;
DROP INDEX tx_category_rule_idx;
ALTER TABLE transactions DROP COLUMN transfer_source, DROP COLUMN category_rule_id;
DROP TABLE rules;
DROP VIEW bank_category_map;
DROP TABLE category_aliases;
