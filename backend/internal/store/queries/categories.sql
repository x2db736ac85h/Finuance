-- name: CreateCategory :one
INSERT INTO categories (id, user_id, name, name_n) VALUES (@id, @user_id, @name, @name_n) RETURNING *;

-- name: UpsertCategory :one
-- Returns the id of the category with this normalised name, creating it if needed. The no-op
-- update makes RETURNING work for an existing row; the first spelling of the name is kept. Callers
-- check category_aliases first (ResolveBankCategory).
INSERT INTO categories AS c (id, user_id, name, name_n) VALUES (@id, @user_id, @name, @name_n)
ON CONFLICT (user_id, name_n) DO UPDATE SET name = c.name
RETURNING id;

-- name: UpsertBankCategory :exec
-- A bank category of an import becomes a category, unless the name has an alias (the user renamed,
-- merged or deleted that category: the alias decides).
INSERT INTO categories (id, user_id, name, name_n)
SELECT @id, @user_id, @name, @name_n
WHERE NOT EXISTS (SELECT 1 FROM category_aliases a WHERE a.user_id = @user_id AND a.name_n = @name_n)
ON CONFLICT (user_id, name_n) DO NOTHING;

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
SELECT EXISTS (SELECT 1 FROM categories WHERE user_id = @user_id AND name_n = @name_n AND id <> @id);

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
WHERE c.user_id = @user_id
GROUP BY c.id
ORDER BY c.name COLLATE pl_natural, c.id;

-- name: ListCategories :many
SELECT * FROM categories WHERE user_id = @user_id ORDER BY name COLLATE pl_natural, id;

-- name: RenameCategory :one
UPDATE categories SET name = @name, name_n = @name_n WHERE user_id = @user_id AND id = @id RETURNING *;

-- name: DeleteCategory :execrows
-- Transactions keep existing with category_id NULL; budgets of the category go with it.
DELETE FROM categories WHERE user_id = @user_id AND id = @id;

-- name: GetCategory :one
SELECT * FROM categories WHERE user_id = @user_id AND id = @id;
