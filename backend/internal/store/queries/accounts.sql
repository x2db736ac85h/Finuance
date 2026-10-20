-- name: CreateAccount :one
INSERT INTO accounts (id, user_id, name, name_n, currency, number_hint, opening_balance_minor, is_demo)
VALUES (@id, @user_id, @name, @name_n, @currency, sqlc.narg(number_hint), @opening_balance_minor, @is_demo)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE user_id = @user_id AND id = @id;

-- name: ListAccounts :many
SELECT * FROM accounts WHERE user_id = @user_id ORDER BY name COLLATE pl_natural, id;

-- name: UpdateAccount :one
UPDATE accounts
SET name = @name, name_n = @name_n, currency = @currency, number_hint = sqlc.narg(number_hint),
    opening_balance_minor = @opening_balance_minor, archived_at = sqlc.narg(archived_at)
WHERE user_id = @user_id AND id = @id
RETURNING *;

-- name: DeleteAccount :execrows
-- Cascades to the account's imports, transactions, annotations and overrides.
DELETE FROM accounts WHERE user_id = @user_id AND id = @id;

-- name: FindAccountsByNumberHint :many
-- Account suggestion (PLAN §4.4): last four digits of the own-account column.
SELECT * FROM accounts WHERE user_id = @user_id AND number_hint = @number_hint AND archived_at IS NULL ORDER BY created_at;
