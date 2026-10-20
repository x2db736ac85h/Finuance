-- name: CreateCategory :one
INSERT INTO categories (id, user_id, name, name_n) VALUES (@id, @user_id, @name, @name_n) RETURNING *;

-- name: UpsertCategory :one
-- Returns the id of the category with this normalised name, creating it if needed. The no-op
-- update makes RETURNING work for an existing row; the first spelling of the name is kept.
INSERT INTO categories AS c (id, user_id, name, name_n) VALUES (@id, @user_id, @name, @name_n)
ON CONFLICT (user_id, name_n) DO UPDATE SET name = c.name
RETURNING id;

-- name: ListCategories :many
SELECT * FROM categories WHERE user_id = @user_id ORDER BY name COLLATE pl_natural, id;

-- name: RenameCategory :one
UPDATE categories SET name = @name, name_n = @name_n WHERE user_id = @user_id AND id = @id RETURNING *;

-- name: DeleteCategory :execrows
-- Transactions keep existing with category_id NULL; budgets of the category go with it.
DELETE FROM categories WHERE user_id = @user_id AND id = @id;

-- name: GetCategory :one
SELECT * FROM categories WHERE user_id = @user_id AND id = @id;
