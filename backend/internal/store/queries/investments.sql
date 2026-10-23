-- Manual investment data (PLAN §4.9). The repository comes with the investments API.

-- name: GetInvestmentProfile :one
SELECT * FROM investment_profile WHERE user_id = @user_id;

-- name: SaveInvestmentProfile :one
-- Optimistic concurrency as in SavePreferences.
INSERT INTO investment_profile AS p (user_id, ppk_employee_minor, ppk_employer_minor, ppk_state_minor, ppk_value_minor,
                                     swr_bp, real_return_bp, version, updated_at)
VALUES (@user_id, @ppk_employee_minor, @ppk_employer_minor, @ppk_state_minor, @ppk_value_minor, @swr_bp, @real_return_bp, 1, @now)
ON CONFLICT (user_id) DO UPDATE
SET ppk_employee_minor = excluded.ppk_employee_minor, ppk_employer_minor = excluded.ppk_employer_minor,
    ppk_state_minor = excluded.ppk_state_minor, ppk_value_minor = excluded.ppk_value_minor,
    swr_bp = excluded.swr_bp, real_return_bp = excluded.real_return_bp,
    version = p.version + 1, updated_at = excluded.updated_at
WHERE p.version = @expected_version
RETURNING version;

-- name: ListInvestmentPositions :many
SELECT * FROM investment_positions WHERE user_id = @user_id ORDER BY position, id;

-- name: CreateInvestmentPosition :one
INSERT INTO investment_positions (id, user_id, position, name, type, invested_minor, value_minor, annual_income_minor, created_at, updated_at)
VALUES (@id, @user_id, @position, @name, @type, @invested_minor, @value_minor, @annual_income_minor, @now, @now)
RETURNING *;

-- name: UpdateInvestmentPosition :one
UPDATE investment_positions
SET position = @position, name = @name, type = @type, invested_minor = @invested_minor, value_minor = @value_minor,
    annual_income_minor = @annual_income_minor, updated_at = @now
WHERE user_id = @user_id AND id = @id
RETURNING *;

-- name: DeleteInvestmentPosition :execrows
DELETE FROM investment_positions WHERE user_id = @user_id AND id = @id;

-- name: DeleteInvestmentPositions :exec
-- PUT /investments replaces the whole list in one transaction.
DELETE FROM investment_positions WHERE user_id = @user_id;
