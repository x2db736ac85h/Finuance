-- Goals (Phase 20, ADR 0031). User-owned with forced RLS.

-- name: InsertGoal :one
INSERT INTO goals (id, user_id, name, kind, target_minor, currency, account_id, category_id, reduce_pct,
                   baseline_months, starts_on, deadline, created_at, updated_at)
VALUES (@id, @user_id, @name, @kind, sqlc.narg(target_minor), @currency, sqlc.narg(account_id), sqlc.narg(category_id),
        sqlc.narg(reduce_pct), @baseline_months, @starts_on, @deadline, @now, @now)
RETURNING *;

-- name: UpdateGoal :one
-- The kind, currency and start stay; version must be the one the caller read.
UPDATE goals SET name = @name, target_minor = sqlc.narg(target_minor), account_id = sqlc.narg(account_id),
       category_id = sqlc.narg(category_id), reduce_pct = sqlc.narg(reduce_pct), baseline_months = @baseline_months,
       deadline = @deadline, status = @status, version = version + 1, updated_at = @now
WHERE user_id = @user_id AND id = @id AND version = @version
RETURNING *;

-- name: GetGoal :one
SELECT * FROM goals WHERE user_id = @user_id AND id = @id;

-- name: GetGoalForUpdate :one
SELECT * FROM goals WHERE user_id = @user_id AND id = @id FOR UPDATE;

-- name: ListGoals :many
SELECT * FROM goals WHERE user_id = @user_id ORDER BY (status = 'active') DESC, deadline, name COLLATE pl_natural, id;

-- name: DeleteGoal :execrows
DELETE FROM goals WHERE user_id = @user_id AND id = @id;

-- name: CountGoals :one
SELECT count(*) FROM goals WHERE user_id = @user_id;

-- name: MergeCategoryGoals :exec
UPDATE goals SET category_id = @into_id::uuid WHERE user_id = @user_id AND category_id = @from_id::uuid;

-- name: GoalNet :one
-- A save goal without an account: income minus spending of every account in the currency between
-- two days (inclusive), transfers between the user's own accounts left out.
SELECT coalesce(sum(amount_minor), 0)::bigint FROM transactions
WHERE user_id = @user_id AND currency = @currency AND NOT is_transfer AND booked_on BETWEEN @from_on AND @to_on;

-- name: GoalAccountGrowth :one
-- A save goal on one account: everything that came in and went out (transfers included).
SELECT coalesce(sum(amount_minor), 0)::bigint FROM transactions
WHERE user_id = @user_id AND account_id = @account_id AND booked_on BETWEEN @from_on AND @to_on;

-- name: GoalCategoryMonths :many
-- A reduce goal: money out per month for the category between two days (inclusive).
SELECT date_trunc('month', booked_on)::date AS month, coalesce(sum(-amount_minor), 0)::bigint AS spent_minor
FROM transactions
WHERE user_id = @user_id AND category_id = @category_id AND amount_minor < 0 AND NOT is_transfer
  AND booked_on BETWEEN @from_on AND @to_on
GROUP BY 1 ORDER BY 1;

-- name: AIGoalIDs :many
-- Goals an assistant change set and nobody undid (the ✦ badge, ADR 0030).
SELECT DISTINCT i.entity_id FROM ai_change_items i JOIN ai_changes ch ON ch.id = i.change_id
WHERE i.user_id = @user_id AND i.entity = 'goal' AND ch.reverted_at IS NULL;
