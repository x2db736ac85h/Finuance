-- Shared groups (Phase 19, ADR 0028). Every statement runs under WithUser: the membership policies
-- of migration 00014 decide what the caller sees and may change; the WHERE clauses below repeat
-- the rule where it matters, so a mistake shows up as "not found" rather than another member's row.

-- name: CreateSharedGroup :one
INSERT INTO shared_groups (id, owner_id, name, color, currency, kind, starts_on, ends_on, budget_minor, created_at)
VALUES (@id, @owner_id, @name, @color, @currency, @kind, sqlc.narg(starts_on), sqlc.narg(ends_on), sqlc.narg(budget_minor), @now)
RETURNING *;

-- name: GetSharedGroup :one
SELECT * FROM shared_groups WHERE id = @id;

-- name: ListMySharedGroups :many
-- The groups the user is in or invited to, with their own membership.
SELECT g.*, m.id AS member_id, m.role, m.state, pg.id AS linked_group_id
FROM shared_groups g
JOIN shared_group_members m ON m.group_id = g.id AND m.user_id = @user_id
LEFT JOIN groups pg ON pg.shared_group_id = g.id AND pg.user_id = @user_id
WHERE m.state <> 'left'
ORDER BY g.archived_at NULLS FIRST, coalesce(g.starts_on, g.created_at::date) DESC, g.id;

-- name: UpdateSharedGroup :one
UPDATE shared_groups
SET name = @name, color = @color, kind = @kind, starts_on = sqlc.narg(starts_on), ends_on = sqlc.narg(ends_on),
    budget_minor = sqlc.narg(budget_minor), archived_at = sqlc.narg(archived_at)
WHERE id = @id
RETURNING *;

-- name: DeleteSharedGroup :execrows
DELETE FROM shared_groups WHERE id = @id;

-- name: CountOwnedSharedGroups :one
SELECT count(*) FROM shared_groups WHERE owner_id = @owner_id AND archived_at IS NULL;

-- name: InsertSharedMember :one
INSERT INTO shared_group_members (id, group_id, user_id, role, state, display_name, invited_by, created_at, joined_at)
VALUES (@id, @group_id, @user_id, @role, @state, @display_name, sqlc.narg(invited_by), @now, sqlc.narg(joined_at))
RETURNING *;

-- name: ListSharedMembers :many
SELECT * FROM shared_group_members WHERE group_id = @group_id ORDER BY created_at, id;

-- name: GetMyMembership :one
SELECT * FROM shared_group_members WHERE group_id = @group_id AND user_id = @user_id;

-- name: GetSharedMember :one
SELECT * FROM shared_group_members WHERE group_id = @group_id AND id = @id;

-- name: SetSharedMemberState :execrows
UPDATE shared_group_members
SET state = @state,
    display_name = coalesce(sqlc.narg(display_name), display_name),
    joined_at = CASE WHEN @state = 'active' THEN @now ELSE joined_at END,
    left_at = CASE WHEN @state = 'left' THEN @now ELSE left_at END
WHERE group_id = @group_id AND id = @id;

-- name: DeleteSharedMember :execrows
DELETE FROM shared_group_members WHERE group_id = @group_id AND id = @id AND state = 'invited';

-- name: ActiveSharedMemberIDs :many
-- The members an equal split goes to, in id order (the remainder cents go to the first ones).
SELECT id FROM shared_group_members WHERE group_id = @group_id AND state = 'active' ORDER BY id;

-- name: InsertSharedExpense :exec
INSERT INTO shared_expenses (id, group_id, added_by, added_member, paid_by, booked_on, amount_minor, payee,
                             category_name, note, origin, split_mode, created_at, updated_at)
VALUES (@id, @group_id, @added_by, @added_member, @paid_by, @booked_on, @amount_minor, @payee,
        @category_name, @note, @origin, @split_mode, @now, @now);

-- name: UpdateSharedExpense :execrows
UPDATE shared_expenses
SET booked_on = @booked_on, amount_minor = @amount_minor, payee = @payee, category_name = @category_name,
    note = @note, paid_by = @paid_by, split_mode = @split_mode, updated_at = @now
WHERE id = @id AND group_id = @group_id;

-- name: DeleteSharedExpense :execrows
DELETE FROM shared_expenses WHERE id = @id;

-- name: GetSharedExpense :one
SELECT * FROM shared_expenses WHERE id = @id;

-- name: ListSharedExpenses :many
SELECT * FROM shared_expenses WHERE group_id = @group_id ORDER BY booked_on DESC, created_at DESC, id;

-- name: DeleteSharedSplits :exec
DELETE FROM shared_expense_splits WHERE expense_id = @expense_id;

-- name: InsertSharedSplit :exec
INSERT INTO shared_expense_splits (expense_id, group_id, member_id, share_minor, weight)
VALUES (@expense_id, @group_id, @member_id, @share_minor, sqlc.narg(weight));

-- name: ListSharedSplits :many
SELECT * FROM shared_expense_splits WHERE group_id = @group_id ORDER BY expense_id, member_id;

-- name: InsertSharedSettlement :one
INSERT INTO shared_settlements (id, group_id, from_member, to_member, amount_minor, settled_on, note, created_by, created_at)
VALUES (@id, @group_id, @from_member, @to_member, @amount_minor, @settled_on, @note, @created_by, @now)
RETURNING *;

-- name: ListSharedSettlements :many
SELECT * FROM shared_settlements WHERE group_id = @group_id ORDER BY settled_on DESC, created_at DESC, id;

-- name: DeleteSharedSettlement :execrows
DELETE FROM shared_settlements WHERE group_id = @group_id AND id = @id;

-- name: LinkGroupToShared :execrows
UPDATE groups SET shared_group_id = sqlc.narg(shared_group_id) WHERE user_id = @user_id AND id = @id;

-- name: GroupLinkedTo :one
SELECT id FROM groups WHERE user_id = @user_id AND shared_group_id = @shared_group_id;

-- name: UnlinkGroupsFromShared :execrows
UPDATE groups SET shared_group_id = NULL WHERE user_id = @user_id AND shared_group_id = @shared_group_id;

-- name: TransactionsInGroup :many
SELECT id FROM transactions WHERE user_id = @user_id AND group_id = @group_id;

-- name: TransactionsForSharing :many
-- The given transactions with what a shared copy needs, the shared group their personal group is
-- linked to, and their current copy (through the user's private link). Only rows that are linked or
-- already shared come back, so a bulk edit without sharing costs one cheap query.
SELECT t.id, t.account_id, t.fingerprint, t.occurrence, t.split_scope, t.booked_on, t.amount_minor, t.currency,
       t.payee, t.note, coalesce(c.name, '')::text AS category_name,
       g.shared_group_id, l.expense_id AS linked_expense, e.group_id AS linked_group
FROM transactions t
LEFT JOIN categories c ON c.id = t.category_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN shared_expense_links l ON l.user_id = t.user_id AND (l.account_id, l.fingerprint, l.occurrence, l.split_scope) = (t.account_id, t.fingerprint, t.occurrence, t.split_scope)
LEFT JOIN shared_expenses e ON e.id = l.expense_id
WHERE t.user_id = @user_id AND t.id = ANY(@ids::uuid[])
  AND (g.shared_group_id IS NOT NULL OR l.expense_id IS NOT NULL)
ORDER BY t.seq;

-- name: UpsertSharedLink :exec
INSERT INTO shared_expense_links (user_id, account_id, fingerprint, occurrence, split_scope, expense_id)
VALUES (@user_id, @account_id, @fingerprint, @occurrence, @split_scope, @expense_id)
ON CONFLICT (account_id, fingerprint, occurrence, split_scope) DO UPDATE SET expense_id = excluded.expense_id;

-- name: SharedCopiesFrom :many
-- The user's copies in a group and whether their source transaction still exists (a missing source
-- is shown to its owner only).
SELECT l.expense_id, (t.id IS NOT NULL)::bool AS source_present
FROM shared_expense_links l
JOIN shared_expenses e ON e.id = l.expense_id
LEFT JOIN transactions t ON t.user_id = l.user_id AND (t.account_id, t.fingerprint, t.occurrence, t.split_scope) = (l.account_id, l.fingerprint, l.occurrence, l.split_scope)
WHERE l.user_id = @user_id AND e.group_id = @group_id;

-- name: RefreshSharedCategories :execrows
-- After the rules ran: the category names of the user's copies follow their transactions.
UPDATE shared_expenses e
SET category_name = coalesce(c.name, ''), updated_at = @now
FROM shared_expense_links l
JOIN transactions t ON t.user_id = l.user_id AND (t.account_id, t.fingerprint, t.occurrence, t.split_scope) = (l.account_id, l.fingerprint, l.occurrence, l.split_scope)
LEFT JOIN categories c ON c.id = t.category_id
WHERE l.user_id = @user_id AND e.id = l.expense_id AND e.added_by = @user_id
  AND e.category_name IS DISTINCT FROM coalesce(c.name, '');

-- name: SharedTimelineStats :many
-- Totals and spans of the groups the user can see (for the timeline).
SELECT e.group_id, count(*)::bigint AS expenses, coalesce(sum(e.amount_minor), 0)::bigint AS total_minor,
       min(e.booked_on)::date AS first_on, max(e.booked_on)::date AS last_on
FROM shared_expenses e
GROUP BY e.group_id;

-- name: GroupCategorySpend :many
-- Money out per personal group and category (the timeline's largest categories and hints).
SELECT t.group_id::uuid AS group_id, coalesce(c.name, '')::text AS category,
       sum(-t.amount_minor)::bigint AS spent_minor, count(*)::bigint AS tx_count
FROM transactions t
LEFT JOIN categories c ON c.id = t.category_id
WHERE t.user_id = @user_id AND t.group_id IS NOT NULL AND t.amount_minor < 0
GROUP BY t.group_id, c.name;

-- name: SharedCategorySpend :many
-- Spending per shared group and category name, over the groups the user can see.
SELECT e.group_id, e.category_name AS category, sum(e.amount_minor)::bigint AS spent_minor, count(*)::bigint AS expenses
FROM shared_expenses e
GROUP BY e.group_id, e.category_name;

-- name: SharedSplitsOf :many
SELECT * FROM shared_expense_splits WHERE expense_id = @expense_id ORDER BY member_id;
