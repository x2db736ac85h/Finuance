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

-- name: SetUserSynthetic :execrows
-- Marks a user as synthetic (the traffic simulator's users, ADR 0041). Never an administrator, and
-- never back: a synthetic user stays one until it is deleted.
UPDATE users SET kind = 'synthetic' WHERE id = @id AND NOT is_admin;

-- name: DeleteUser :execrows
-- Cascades to every user-owned table.
DELETE FROM users WHERE id = @id;

-- name: DeleteDemoUser :execrows
-- Removes a demo user whose seeding failed; never touches a normal account.
DELETE FROM users WHERE id = @id AND is_demo;

-- name: DeleteDemoUsersCreatedBefore :execrows
-- Periodic cleanup of demo visitors (PLAN 19.1); cascades to all of their data.
DELETE FROM users WHERE is_demo AND created_at < @cutoff;
