-- name: CreateCategory :one
INSERT INTO categories (id, user_id, name, name_n) VALUES (@id, @user_id, @name, @name_n) RETURNING *;

-- name: UpsertCategory :one
-- Returns the id of the category with this normalised name, creating it if needed. The no-op
-- update makes RETURNING work for an existing row; the first spelling of the name is kept. Callers
-- check category_aliases first (ResolveBankCategory).
INSERT INTO categories AS c (id, user_id, name, name_n) VALUES (@id, @user_id, @name, @name_n)
ON CONFLICT (user_id, name_n) WHERE archived_at IS NULL DO UPDATE SET name = c.name
RETURNING id;

-- name: UpsertBankCategory :exec
-- A bank category of an import becomes a category, unless the name has an alias (the user renamed,
-- merged or deleted that category: the alias decides).
INSERT INTO categories (id, user_id, name, name_n)
SELECT @id, @user_id, @name, @name_n
WHERE NOT EXISTS (SELECT 1 FROM category_aliases a WHERE a.user_id = @user_id AND a.name_n = @name_n)
ON CONFLICT (user_id, name_n) WHERE archived_at IS NULL DO NOTHING;

-- name: GetCategoryAlias :one
SELECT category_id FROM category_aliases WHERE user_id = @user_id AND name_n = @name_n;

-- name: SetCategoryAlias :exec
-- name_n now means category_id (NULL: uncategorised).
INSERT INTO category_aliases (user_id, name_n, category_id) VALUES (@user_id, @name_n, sqlc.narg(category_id))
ON CONFLICT (user_id, name_n) DO UPDATE SET category_id = excluded.category_id;

-- name: DeleteCategoryAlias :exec
DELETE FROM category_aliases WHERE user_id = @user_id AND name_n = @name_n;

-- name: MoveCategoryAliases :exec
UPDATE category_aliases SET category_id = @into_id::uuid WHERE user_id = @user_id AND category_id = @from_id::uuid;

-- name: CategoryNameTaken :one
-- Among the categories in use: an archived category's name is free (migration 00030).
SELECT EXISTS (SELECT 1 FROM categories WHERE user_id = @user_id AND name_n = @name_n AND id <> @id AND archived_at IS NULL);

-- name: MergeCategoryTransactions :execrows
-- Merging: the transactions keep how their category was set (bank, rule, manual).
UPDATE transactions SET category_id = @into_id::uuid WHERE user_id = @user_id AND category_id = @from_id::uuid;

-- name: MergeCategoryAnnotations :exec
UPDATE tx_annotations SET category_id = @into_id::uuid WHERE user_id = @user_id AND category_id = @from_id::uuid;

-- name: MergeCategoryRules :exec
UPDATE rules SET set_category_id = @into_id::uuid WHERE user_id = @user_id AND set_category_id = @from_id::uuid;

-- name: MergeCategoryBudget :exec
-- The merged category's budget moves over when the target has none (else the target's stays).
UPDATE budgets b SET category_id = @into_id::uuid
WHERE b.user_id = @user_id AND b.category_id = @from_id::uuid
  AND NOT EXISTS (SELECT 1 FROM budgets x WHERE x.user_id = @user_id AND x.category_id = @into_id::uuid);

-- name: UncategoriseCategoryTransactions :execrows
-- Deleting a category: its transactions become uncategorised, however their category was set.
UPDATE transactions SET category_id = NULL, category_source = 'none', category_rule_id = NULL
WHERE user_id = @user_id AND category_id = @id::uuid;

-- name: CategoryStats :many
-- Settings → Categories: every category with its transaction count, how many were set by hand and
-- by rules, and the bank names that map to it (its own name excluded).
SELECT c.id, c.name,
       count(t.id)::bigint AS tx_count,
       count(t.id) FILTER (WHERE t.category_source = 'manual')::bigint AS manual_count,
       count(t.id) FILTER (WHERE t.category_source = 'rule')::bigint AS rule_count,
       coalesce((SELECT array_agg(a.name_n ORDER BY a.name_n) FROM category_aliases a
                 WHERE a.user_id = c.user_id AND a.category_id = c.id), '{}')::text[] AS aliases
FROM categories c
LEFT JOIN transactions t ON t.user_id = c.user_id AND t.category_id = c.id
WHERE c.user_id = @user_id AND c.archived_at IS NULL
GROUP BY c.id
ORDER BY c.name COLLATE pl_natural, c.id;

-- name: ListCategories :many
SELECT * FROM categories WHERE user_id = @user_id AND archived_at IS NULL ORDER BY name COLLATE pl_natural, id;

-- name: RenameCategory :one
UPDATE categories SET name = @name, name_n = @name_n WHERE user_id = @user_id AND id = @id AND archived_at IS NULL RETURNING *;

-- name: DeleteCategory :execrows
-- Transactions keep existing with category_id NULL; budgets of the category go with it.
DELETE FROM categories WHERE user_id = @user_id AND id = @id;

-- name: GetCategory :one
-- A category in use. An archived one is not found, like a deleted one.
SELECT * FROM categories WHERE user_id = @user_id AND id = @id AND archived_at IS NULL;

-- name: GetCategoryForArchive :one
-- In use or archived, locked: archiving, unarchiving and deleting for good each look at archived_at.
SELECT * FROM categories WHERE user_id = @user_id AND id = @id FOR UPDATE;

-- name: SetCategoryArchived :exec
UPDATE categories SET archived_at = sqlc.narg(archived_at) WHERE user_id = @user_id AND id = @id;

-- name: ArchivedCategories :many
-- Settings → Categories, the archived list: what unarchiving would put back, by kind.
SELECT c.id, c.name, c.archived_at,
       count(i.id) FILTER (WHERE i.kind = 'annotation')::bigint AS labels,
       count(i.id) FILTER (WHERE i.kind = 'rule')::bigint AS rules,
       count(i.id) FILTER (WHERE i.kind = 'alias')::bigint AS bank_names,
       count(i.id) FILTER (WHERE i.kind = 'budget')::bigint AS budgets,
       count(i.id) FILTER (WHERE i.kind = 'goal')::bigint AS goals
FROM categories c
LEFT JOIN category_archive_items i ON i.category_id = c.id AND i.user_id = c.user_id
WHERE c.user_id = @user_id AND c.archived_at IS NOT NULL
GROUP BY c.id
ORDER BY c.archived_at DESC, c.id;

-- Archiving (ledger.ArchiveCategory): each source of the category is written down, then taken
-- away. One statement per kind, so a category with many labels costs no loop.

-- name: ArchiveCategoryAnnotations :exec
WITH taken AS (
  UPDATE tx_annotations a SET category_id = NULL, updated_at = @now
  WHERE a.user_id = @user_id AND a.category_id = @id::uuid
  RETURNING a.account_id, a.fingerprint, a.occurrence, a.split_scope
)
INSERT INTO category_archive_items (user_id, category_id, kind, account_id, fingerprint, occurrence, split_scope)
SELECT @user_id, @id::uuid, 'annotation', account_id, fingerprint, occurrence, split_scope FROM taken;

-- name: ArchiveCategoryRules :exec
WITH taken AS (
  UPDATE rules r SET set_category_id = NULL WHERE r.user_id = @user_id AND r.set_category_id = @id::uuid RETURNING r.id
)
INSERT INTO category_archive_items (user_id, category_id, kind, ref_id)
SELECT @user_id, @id::uuid, 'rule', id FROM taken;

-- name: ArchiveCategoryGoals :exec
WITH taken AS (
  UPDATE goals g SET category_id = NULL WHERE g.user_id = @user_id AND g.category_id = @id::uuid RETURNING g.id
)
INSERT INTO category_archive_items (user_id, category_id, kind, ref_id)
SELECT @user_id, @id::uuid, 'goal', id FROM taken;

-- name: ArchiveCategoryBudget :exec
WITH taken AS (
  DELETE FROM budgets b WHERE b.user_id = @user_id AND b.category_id = @id::uuid RETURNING b.amount_minor
)
INSERT INTO category_archive_items (user_id, category_id, kind, amount_minor)
SELECT @user_id, @id::uuid, 'budget', amount_minor FROM taken;

-- name: ArchiveCategoryAliases :exec
-- The bank names that meant this category now mean "uncategorised", as after a delete.
WITH taken AS (
  UPDATE category_aliases a SET category_id = NULL WHERE a.user_id = @user_id AND a.category_id = @id::uuid RETURNING a.name_n
)
INSERT INTO category_archive_items (user_id, category_id, kind, name_n)
SELECT @user_id, @id::uuid, 'alias', name_n FROM taken;

-- name: ArchiveCategoryOwnName :exec
-- Its own name gets an alias to nothing, so the bank's rows of that name stay uncategorised. Written
-- down only when the alias is new: unarchiving removes exactly what this added.
WITH added AS (
  INSERT INTO category_aliases (user_id, name_n, category_id) VALUES (@user_id, @name_n, NULL)
  ON CONFLICT (user_id, name_n) DO NOTHING RETURNING name_n
)
INSERT INTO category_archive_items (user_id, category_id, kind, name_n)
SELECT @user_id, @id::uuid, 'own_name', name_n FROM added;

-- Unarchiving (ledger.UnarchiveCategory): each recorded source comes back where nothing has taken
-- its place since.

-- name: RestoreCategoryAnnotations :execrows
-- A label comes back where the transaction has no label set by hand now (the annotation may have
-- been deleted as empty in between) and its account still exists.
INSERT INTO tx_annotations (account_id, fingerprint, occurrence, split_scope, user_id, category_id, updated_at)
SELECT i.account_id, i.fingerprint, i.occurrence, i.split_scope, i.user_id, i.category_id, @now
FROM category_archive_items i JOIN accounts acc ON acc.id = i.account_id AND acc.user_id = i.user_id
WHERE i.user_id = @user_id AND i.category_id = @id::uuid AND i.kind = 'annotation'
ON CONFLICT (account_id, fingerprint, occurrence, split_scope) DO UPDATE
  SET category_id = excluded.category_id, updated_at = excluded.updated_at
  WHERE tx_annotations.category_id IS NULL;

-- name: RestoreCategoryRules :execrows
UPDATE rules r SET set_category_id = i.category_id
FROM category_archive_items i
WHERE i.user_id = @user_id AND i.category_id = @id::uuid AND i.kind = 'rule'
  AND r.user_id = i.user_id AND r.id = i.ref_id AND r.set_category_id IS NULL;

-- name: RestoreCategoryGoals :execrows
UPDATE goals g SET category_id = i.category_id
FROM category_archive_items i
WHERE i.user_id = @user_id AND i.category_id = @id::uuid AND i.kind = 'goal'
  AND g.user_id = i.user_id AND g.id = i.ref_id AND g.category_id IS NULL;

-- name: RestoreCategoryBudget :execrows
INSERT INTO budgets (user_id, category_id, amount_minor, updated_at)
SELECT i.user_id, i.category_id, i.amount_minor, @now
FROM category_archive_items i
WHERE i.user_id = @user_id AND i.category_id = @id::uuid AND i.kind = 'budget'
ON CONFLICT (user_id, category_id) DO NOTHING;

-- name: RestoreCategoryAliases :execrows
-- A bank name means the category again while it still means "uncategorised".
UPDATE category_aliases a SET category_id = i.category_id
FROM category_archive_items i
WHERE i.user_id = @user_id AND i.category_id = @id::uuid AND i.kind = 'alias'
  AND a.user_id = i.user_id AND a.name_n = i.name_n AND a.category_id IS NULL;

-- name: RestoreCategoryOwnName :exec
-- The alias to nothing that archiving added for its own name goes, so the name means it again.
DELETE FROM category_aliases a USING category_archive_items i
WHERE i.user_id = @user_id AND i.category_id = @id::uuid AND i.kind = 'own_name'
  AND a.user_id = i.user_id AND a.name_n = i.name_n AND a.category_id IS NULL;

-- name: RestoreCategoryManualTransactions :execrows
-- Transactions whose label came back and that are still uncategorised carry it again. (A
-- reclassification recomputes only what rules set, so labels and bank names are put back here.)
UPDATE transactions t
SET category_id = a.category_id, category_source = 'manual', category_rule_id = NULL, updated_at = @now
FROM category_archive_items i
JOIN tx_annotations a ON a.account_id = i.account_id AND a.fingerprint = i.fingerprint
  AND a.occurrence = i.occurrence AND a.split_scope = i.split_scope AND a.category_id = i.category_id
WHERE i.user_id = @user_id AND i.category_id = @id::uuid AND i.kind = 'annotation'
  AND t.user_id = i.user_id AND t.account_id = i.account_id AND t.fingerprint = i.fingerprint
  AND t.occurrence = i.occurrence AND t.split_scope = i.split_scope AND t.category_source = 'none';

-- name: RestoreCategoryBankTransactions :execrows
-- Uncategorised transactions whose bank category name means this category again get it, as an
-- import would give it to them.
UPDATE transactions t
SET category_id = m.category_id, category_source = 'bank', category_rule_id = NULL, updated_at = @now
FROM bank_category_map m
WHERE t.user_id = @user_id AND t.category_source = 'none' AND t.bank_category_n <> ''
  AND m.user_id = t.user_id AND m.name_n = t.bank_category_n AND m.category_id = @id::uuid;

-- name: ClearCategoryArchive :exec
DELETE FROM category_archive_items WHERE user_id = @user_id AND category_id = @id::uuid;

-- name: ListCategoryAliases :many
-- Every bank category name with the category it now goes to (NULL: uncategorised), for the data export.
SELECT a.name_n, a.category_id, c.name AS category_name FROM category_aliases a
LEFT JOIN categories c ON c.id = a.category_id
WHERE a.user_id = @user_id ORDER BY a.name_n;
