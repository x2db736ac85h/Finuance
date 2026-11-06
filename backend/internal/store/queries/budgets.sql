-- Budgets are global per category (legacy behaviour).

-- name: SetBudget :exec
INSERT INTO budgets (user_id, category_id, amount_minor, updated_at) VALUES (@user_id, @category_id, @amount_minor, @now)
ON CONFLICT (user_id, category_id) DO UPDATE SET amount_minor = excluded.amount_minor, updated_at = excluded.updated_at;

-- name: ListBudgets :many
SELECT b.category_id, c.name, b.amount_minor, b.updated_at
FROM budgets b JOIN categories c ON c.id = b.category_id
WHERE b.user_id = @user_id
ORDER BY c.name COLLATE pl_natural, b.category_id;

-- name: DeleteBudget :execrows
DELETE FROM budgets WHERE user_id = @user_id AND category_id = @category_id;

-- name: BudgetAmountForUpdate :one
-- A category's budget before an assistant change sets it (ADR 0030).
SELECT amount_minor FROM budgets WHERE user_id = @user_id AND category_id = @category_id FOR UPDATE;
