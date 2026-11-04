-- Transaction groups (PLAN §4.7).

-- name: CreateGroup :one
INSERT INTO groups (id, user_id, name, name_n, color) VALUES (@id, @user_id, @name, @name_n, @color) RETURNING *;

-- name: GetGroup :one
SELECT * FROM groups WHERE user_id = @user_id AND id = @id;

-- name: ListGroups :many
SELECT * FROM groups WHERE user_id = @user_id ORDER BY name COLLATE pl_natural, id;

-- name: UpdateGroup :one
UPDATE groups SET name = @name, name_n = @name_n, color = @color WHERE user_id = @user_id AND id = @id RETURNING *;

-- name: SetGroupBudget :execrows
-- NULL removes the budget.
UPDATE groups SET budget_minor = sqlc.narg(budget_minor)::bigint WHERE user_id = @user_id AND id = @id;

-- name: DeleteGroup :execrows
-- Removes the group and its labels only (ON DELETE SET NULL on transactions and annotations).
DELETE FROM groups WHERE user_id = @user_id AND id = @id;

-- name: GroupStats :many
-- Every group with its transactions over all the user's accounts, ignoring filters (PLAN §4.7):
-- count, money out and in (minor units, transfers included as in the legacy), first and last day.
-- The currency is the most common one among the group's transactions.
SELECT g.id, g.name, g.color, g.created_at, g.budget_minor, g.shared_group_id,
       count(t.id)::bigint AS tx_count,
       coalesce(sum(-t.amount_minor) FILTER (WHERE t.amount_minor < 0), 0)::bigint AS spent_minor,
       coalesce(sum(t.amount_minor) FILTER (WHERE t.amount_minor > 0), 0)::bigint AS income_minor,
       min(t.booked_on)::date AS first_on,
       max(t.booked_on)::date AS last_on,
       coalesce(mode() WITHIN GROUP (ORDER BY t.currency), '')::text AS currency
FROM groups g
LEFT JOIN transactions t ON t.group_id = g.id AND t.user_id = @user_id
WHERE g.user_id = @user_id
GROUP BY g.id
ORDER BY g.name COLLATE pl_natural, g.id;
