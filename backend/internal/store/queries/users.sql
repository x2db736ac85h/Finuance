-- users and sessions are outside RLS (PLAN §5.3); only the auth package uses these queries,
-- through store.WithoutUser.

-- name: CreateUser :one
INSERT INTO users (id, email, password_hash, display_name, is_demo)
VALUES (@id, @email, @password_hash, @display_name, @is_demo)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByEmail :one
-- email is citext, so the lookup ignores case.
SELECT * FROM users WHERE email = @email;

-- name: SetUserLastLogin :exec
UPDATE users SET last_login_at = @at WHERE id = @id;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = @password_hash WHERE id = @id;

-- name: SetUserDisplayName :exec
UPDATE users SET display_name = @display_name WHERE id = @id;

-- name: DeleteUser :execrows
-- Cascades to every user-owned table.
DELETE FROM users WHERE id = @id;
