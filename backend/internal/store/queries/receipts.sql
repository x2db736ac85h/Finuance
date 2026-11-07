-- Receipts by photo (Phase 20, ADR 0032).

-- name: ReceiptCandidates :many
-- Money out of exactly this amount, booked within the days around the receipt's date, nearest first.
SELECT t.id, t.account_id, a.name AS account_name, a.kind AS account_kind, t.booked_on, t.payee, t.description,
       t.amount_minor, t.currency, coalesce(c.name, '')::text AS category, t.note, t.origin
FROM transactions t
JOIN accounts a ON a.user_id = t.user_id AND a.id = t.account_id
LEFT JOIN categories c ON c.user_id = t.user_id AND c.id = t.category_id
WHERE t.user_id = @user_id AND t.amount_minor = @amount_minor AND t.booked_on BETWEEN @from_on AND @to_on
ORDER BY abs(t.booked_on - @receipt_on::date), t.booked_on, t.id
LIMIT 50;

-- name: MergeSuggestions :many
-- Typed rows of bank accounts with an imported row of the same account and amount booked within 3
-- days, not dismissed: the nearest imported row first for each typed one.
SELECT m.id AS typed_id, m.booked_on AS typed_on, m.payee AS typed_payee, m.note AS typed_note,
       i.id AS imported_id, i.booked_on AS imported_on, i.payee AS imported_payee,
       m.amount_minor, m.currency, a.id AS account_id, a.name AS account_name
FROM transactions m
JOIN accounts a ON a.user_id = m.user_id AND a.id = m.account_id AND a.kind = 'bank'
JOIN transactions i ON i.user_id = m.user_id AND i.account_id = m.account_id AND i.origin = 'import'
  AND i.amount_minor = m.amount_minor AND i.booked_on BETWEEN m.booked_on - 3 AND m.booked_on + 3
WHERE m.user_id = @user_id AND m.origin = 'manual'
  AND NOT EXISTS (SELECT 1 FROM merge_dismissals d WHERE d.user_id = m.user_id AND d.typed_id = m.id AND d.imported_id = i.id)
ORDER BY m.booked_on DESC, m.id, abs(i.booked_on - m.booked_on), i.id
LIMIT 200;

-- name: DismissMerge :exec
INSERT INTO merge_dismissals (user_id, typed_id, imported_id, created_at) VALUES (@user_id, @typed_id, @imported_id, @now)
ON CONFLICT DO NOTHING;

-- name: TransactionForMerge :one
SELECT id, account_id, fingerprint, occurrence, split_scope, origin, booked_on, amount_minor, category_id, category_source,
       group_id, note
FROM transactions WHERE user_id = @user_id AND id = @id FOR UPDATE;

-- name: SharedLinkOf :one
SELECT expense_id FROM shared_expense_links
WHERE user_id = @user_id AND account_id = @account_id AND fingerprint = @fingerprint AND occurrence = @occurrence
  AND split_scope = @split_scope;

-- name: MoveSharedLink :exec
UPDATE shared_expense_links SET fingerprint = @to_fingerprint, occurrence = @to_occurrence, split_scope = @to_split_scope
WHERE user_id = @user_id AND account_id = @account_id AND fingerprint = @fingerprint AND occurrence = @occurrence
  AND split_scope = @split_scope;
