-- Transaction groups (PLAN §4.7).

-- name: CreateGroup :one
INSERT INTO groups (id, user_id, name, name_n, color) VALUES (@id, @user_id, @name, @name_n, @color) RETURNING *;

-- name: GetGroup :one
SELECT * FROM groups WHERE user_id = @user_id AND id = @id;

-- name: ListGroups :many
SELECT * FROM groups WHERE user_id = @user_id ORDER BY name COLLATE pl_natural, id;

-- name: UpdateGroup :one
UPDATE groups SET name = @name, name_n = @name_n, color = @color WHERE user_id = @user_id AND id = @id RETURNING *;

-- name: DeleteGroup :execrows
-- Removes the group and its labels only (ON DELETE SET NULL on transactions and annotations).
DELETE FROM groups WHERE user_id = @user_id AND id = @id;
