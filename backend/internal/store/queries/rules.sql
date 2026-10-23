-- Rules and transfers (PLAN tasks 15.2, 15.3). The dynamic part, a rule's match, is compiled by
-- internal/query; these are the fixed statements around it.

-- name: ListRules :many
SELECT r.*, c.name AS category_name FROM rules r
LEFT JOIN categories c ON c.id = r.set_category_id
WHERE r.user_id = @user_id ORDER BY r.position, r.created_at, r.id;

-- name: GetRule :one
SELECT r.*, c.name AS category_name FROM rules r
LEFT JOIN categories c ON c.id = r.set_category_id
WHERE r.user_id = @user_id AND r.id = @id;

-- name: CountRules :one
SELECT count(*) FROM rules WHERE user_id = @user_id;

-- name: CreateRule :exec
-- A new rule goes last.
INSERT INTO rules (id, user_id, name, enabled, position, match, set_category_id, mark_transfer, created_at, updated_at)
VALUES (@id, @user_id, @name, @enabled,
        (SELECT coalesce(max(position), -1) + 1 FROM rules WHERE user_id = @user_id),
        @match, sqlc.narg(set_category_id), @mark_transfer, @now, @now);

-- name: UpdateRule :execrows
UPDATE rules SET name = @name, enabled = @enabled, match = @match, set_category_id = sqlc.narg(set_category_id),
                 mark_transfer = @mark_transfer, updated_at = @now
WHERE user_id = @user_id AND id = @id;

-- name: DeleteRule :execrows
DELETE FROM rules WHERE user_id = @user_id AND id = @id;

-- name: SetRulePositions :execrows
-- Reorder: ids in their new order (positions 0, 1, …).
UPDATE rules r SET position = o.ord - 1, updated_at = @now
FROM unnest(@ids::uuid[]) WITH ORDINALITY AS o(id, ord)
WHERE r.user_id = @user_id AND r.id = o.id;

-- name: LockUserAccounts :exec
-- Reclassifying writes to every account of the user: it takes each account's writer lock (ADR 0013)
-- in id order, so it waits for commits, deletes and reprocesses and they wait for it.
SELECT id FROM accounts WHERE user_id = @user_id ORDER BY id FOR UPDATE;

-- name: ResetRuleCategories :execrows
-- Reclassify step 1: categories set by rules go back to the bank's category.
UPDATE transactions t
SET category_id = m.category_id,
    category_source = CASE WHEN m.category_id IS NULL THEN 'none' ELSE 'bank' END,
    category_rule_id = NULL
FROM transactions x
LEFT JOIN bank_category_map m ON m.user_id = x.user_id AND m.name_n = x.bank_category_n AND x.bank_category_n <> ''
WHERE t.user_id = @user_id AND t.category_source = 'rule' AND x.id = t.id;

-- name: ResetAutomaticTransfers :execrows
-- Reclassify step 2: transfers marked by rules, and pairs that lost their other half (deleted or
-- re-imported), become unmarked again. Pairs whose both halves remain stay as they are.
UPDATE transactions SET is_transfer = false, transfer_source = 'none', transfer_peer_id = NULL
WHERE user_id = @user_id
  AND (transfer_source = 'rule' OR (transfer_source = 'pair' AND transfer_peer_id IS NULL));

-- name: TransferCandidates :many
-- Every unmarked outgoing transaction with an unmarked incoming one of the opposite amount in the
-- same currency, in another account of the user, booked at most 3 days apart; smallest gap first,
-- then the older rows (seq), so the greedy pairing in Go is deterministic.
SELECT a.id AS out_id, b.id AS in_id
FROM transactions a
JOIN transactions b ON b.user_id = a.user_id AND b.amount_minor = -a.amount_minor AND b.currency = a.currency
  AND b.account_id <> a.account_id AND b.transfer_source = 'none'
  AND b.booked_on BETWEEN a.booked_on - 3 AND a.booked_on + 3
WHERE a.user_id = @user_id AND a.amount_minor < 0 AND a.transfer_source = 'none'
ORDER BY abs(a.booked_on - b.booked_on), a.seq, b.seq;

-- name: PairTransfers :execrows
-- Mark detected pairs: out_ids[i] and in_ids[i] point at each other.
UPDATE transactions t SET is_transfer = true, transfer_source = 'pair', transfer_peer_id = p.peer
FROM (SELECT o.id, i.id AS peer FROM unnest(@out_ids::uuid[]) WITH ORDINALITY AS o(id, n)
      JOIN unnest(@in_ids::uuid[]) WITH ORDINALITY AS i(id, n) USING (n)
      UNION ALL
      SELECT i.id, o.id FROM unnest(@out_ids::uuid[]) WITH ORDINALITY AS o(id, n)
      JOIN unnest(@in_ids::uuid[]) WITH ORDINALITY AS i(id, n) USING (n)) p
WHERE t.user_id = @user_id AND t.id = p.id;

-- name: TransferPeers :many
-- The other halves of the given transactions' pairs (unpairing one side unpairs both).
SELECT transfer_peer_id::uuid AS peer_id FROM transactions
WHERE user_id = @user_id AND id = ANY(@ids::uuid[]) AND transfer_source = 'pair' AND transfer_peer_id IS NOT NULL;

-- name: SetManualTransfer :many
-- The user decides: is_transfer true or false (transfer_source 'manual'), or NULL for automatic
-- again (unmarked until the next reclassify). Returns the dedupe identities for tx_annotations.
UPDATE transactions
SET is_transfer = coalesce(sqlc.narg(is_transfer)::bool, false),
    transfer_source = CASE WHEN sqlc.narg(is_transfer)::bool IS NULL THEN 'none' ELSE 'manual' END,
    transfer_peer_id = NULL,
    updated_at = @now
WHERE user_id = @user_id AND id = ANY(@ids::uuid[])
  -- a detected pair is already a transfer: only "not a transfer" changes it
  AND (transfer_source <> 'pair' OR sqlc.narg(is_transfer)::bool IS FALSE)
RETURNING id, account_id, fingerprint, occurrence, split_scope;

-- name: SetAnnotationTransfer :exec
-- Mirror a manual transfer decision into tx_annotations (it survives delete + re-import).
INSERT INTO tx_annotations (account_id, fingerprint, occurrence, split_scope, user_id, transfer_override, updated_at)
VALUES (@account_id, @fingerprint, @occurrence, @split_scope, @user_id, sqlc.narg(transfer_override), @now)
ON CONFLICT (account_id, fingerprint, occurrence, split_scope)
DO UPDATE SET transfer_override = excluded.transfer_override, updated_at = excluded.updated_at;
