-- Phase 26 (26.U, U7): a category is archived, not deleted. An archived category behaves as if it
-- were removed: nothing refers to it, no list offers it, and its name is free for a new category.
-- Unarchiving puts back what the archive took away and nobody has changed since.
--
-- A transaction's category is derived (ADR 0017): from a label set by hand (tx_annotations), from
-- a rule, or from the bank's category name (bank_category_map). So the archive records those
-- sources, not transactions. Unarchiving gives the category back to the transactions that are still
-- uncategorised (by label, by bank name, and by rule through the reclassification that follows).
--   categories.archived_at   when it was archived; NULL for a category in use.
--   category_archive_items   what the archive took away:
--     annotation   a label set by hand (the annotation's key)
--     rule         a rule whose action was this category (ref_id)
--     alias        a bank name that meant this category (name_n)
--     own_name     its own name, when no alias of that name existed before (name_n)
--     budget       its budget (amount_minor)
--     goal         a goal about this category (ref_id)
-- The name's uniqueness holds among categories in use only. No new reference to an archived
-- category can be made (a trigger on the five tables that hold one; it answers like a missing
-- category, SQLSTATE 23503).

-- +goose Up
ALTER TABLE categories ADD COLUMN archived_at timestamptz;
ALTER TABLE categories DROP CONSTRAINT categories_user_id_name_n_key;
CREATE UNIQUE INDEX categories_user_name_active_idx ON categories (user_id, name_n) WHERE archived_at IS NULL;

CREATE TABLE category_archive_items (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category_id   uuid NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  kind          text NOT NULL CHECK (kind IN ('annotation', 'rule', 'alias', 'own_name', 'budget', 'goal')),
  ref_id        uuid,
  account_id    uuid,
  fingerprint   bytea,
  occurrence    integer,
  split_scope   uuid,
  name_n        text,
  amount_minor  bigint CHECK (amount_minor >= 0),
  CHECK (kind <> 'annotation' OR (account_id IS NOT NULL AND fingerprint IS NOT NULL AND occurrence IS NOT NULL AND split_scope IS NOT NULL)),
  CHECK (kind NOT IN ('rule', 'goal') OR ref_id IS NOT NULL),
  CHECK (kind NOT IN ('alias', 'own_name') OR name_n IS NOT NULL),
  CHECK (kind <> 'budget' OR amount_minor IS NOT NULL)
);
CREATE INDEX category_archive_items_category_idx ON category_archive_items (category_id, kind);
CREATE INDEX category_archive_items_user_idx ON category_archive_items (user_id);
ALTER TABLE category_archive_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE category_archive_items FORCE ROW LEVEL SECURITY;
CREATE POLICY category_archive_items_tenant ON category_archive_items
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

CREATE OR REPLACE VIEW bank_category_map WITH (security_invoker = true) AS
  SELECT a.user_id, a.name_n, a.category_id FROM category_aliases a
  UNION ALL
  SELECT c.user_id, c.name_n, c.id AS category_id FROM categories c
  WHERE c.archived_at IS NULL
    AND NOT EXISTS (SELECT 1 FROM category_aliases a WHERE a.user_id = c.user_id AND a.name_n = c.name_n);

-- +goose StatementBegin
CREATE FUNCTION refuse_archived_category() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  ref uuid := (to_jsonb(NEW) ->> TG_ARGV[0])::uuid;
BEGIN
  IF ref IS NOT NULL AND EXISTS (SELECT 1 FROM categories c WHERE c.id = ref AND c.archived_at IS NOT NULL) THEN
    RAISE EXCEPTION 'category % is archived', ref USING ERRCODE = 'foreign_key_violation';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER refuse_archived_category BEFORE INSERT OR UPDATE OF category_id ON tx_annotations
  FOR EACH ROW WHEN (NEW.category_id IS NOT NULL) EXECUTE FUNCTION refuse_archived_category('category_id');
CREATE TRIGGER refuse_archived_category BEFORE INSERT OR UPDATE OF category_id ON budgets
  FOR EACH ROW EXECUTE FUNCTION refuse_archived_category('category_id');
CREATE TRIGGER refuse_archived_category BEFORE INSERT OR UPDATE OF category_id ON goals
  FOR EACH ROW WHEN (NEW.category_id IS NOT NULL) EXECUTE FUNCTION refuse_archived_category('category_id');
CREATE TRIGGER refuse_archived_category BEFORE INSERT OR UPDATE OF category_id ON category_aliases
  FOR EACH ROW WHEN (NEW.category_id IS NOT NULL) EXECUTE FUNCTION refuse_archived_category('category_id');
CREATE TRIGGER refuse_archived_category BEFORE INSERT OR UPDATE OF set_category_id ON rules
  FOR EACH ROW WHEN (NEW.set_category_id IS NOT NULL) EXECUTE FUNCTION refuse_archived_category('set_category_id');

-- +goose Down
DROP TRIGGER refuse_archived_category ON rules;
DROP TRIGGER refuse_archived_category ON category_aliases;
DROP TRIGGER refuse_archived_category ON goals;
DROP TRIGGER refuse_archived_category ON budgets;
DROP TRIGGER refuse_archived_category ON tx_annotations;
DROP FUNCTION refuse_archived_category();
CREATE OR REPLACE VIEW bank_category_map WITH (security_invoker = true) AS
  SELECT a.user_id, a.name_n, a.category_id FROM category_aliases a
  UNION ALL
  SELECT c.user_id, c.name_n, c.id AS category_id FROM categories c
  WHERE NOT EXISTS (SELECT 1 FROM category_aliases a WHERE a.user_id = c.user_id AND a.name_n = c.name_n);
DROP TABLE category_archive_items;
-- going back, an archived category is a deleted one: its name may be in use again
DELETE FROM categories WHERE archived_at IS NOT NULL;
DROP INDEX categories_user_name_active_idx;
ALTER TABLE categories ADD CONSTRAINT categories_user_id_name_n_key UNIQUE (user_id, name_n);
ALTER TABLE categories DROP COLUMN archived_at;
