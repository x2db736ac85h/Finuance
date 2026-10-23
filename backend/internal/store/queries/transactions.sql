-- name: GetTransaction :one
-- Detail view: the transaction with its category and group names, the rule that set the category
-- and the other half of a transfer pair.
SELECT t.*, c.name AS category_name, g.name AS group_name, g.color AS group_color, r.name AS rule_name,
       p.booked_on AS peer_booked_on, p.amount_minor AS peer_amount_minor, pa.name AS peer_account_name
FROM transactions t
LEFT JOIN categories c ON c.id = t.category_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN rules r ON r.id = t.category_rule_id
LEFT JOIN transactions p ON p.id = t.transfer_peer_id
LEFT JOIN accounts pa ON pa.id = p.account_id
WHERE t.user_id = @user_id AND t.id = @id;

-- name: TransactionProvenance :many
-- The staged rows (one per import that contains this transaction), oldest import first.
SELECT ir.import_id, ir.row_index, i.filename, i.committed_at, ir.raw
FROM import_rows ir
JOIN imports i ON i.id = ir.import_id
WHERE ir.user_id = @user_id AND ir.transaction_id = @transaction_id
ORDER BY i.committed_at, ir.import_id;

-- name: CountEligibleImportRows :one
-- How many staged rows step 1 of the commit may insert; the caller generates that many ids.
SELECT count(*) FROM import_rows ir
WHERE ir.user_id = @user_id AND ir.import_id = @import_id AND ir.parse_error IS NULL
  AND ir.fingerprint NOT IN (SELECT d.fingerprint FROM dedupe_overrides d WHERE d.user_id = @user_id AND d.account_id = @account_id);

-- name: InsertTransactionsFromImport :many
-- Commit step 1 (PLAN §4.4): new transactions from the staged rows, in file order. Errored rows and
-- fingerprints with a dedupe override are skipped (the override path re-materialises them). A row
-- whose (fingerprint, occurrence) already exists in the account is a duplicate: ON CONFLICT skips
-- it. ids holds one pre-generated UUIDv7 per eligible row (CountEligibleImportRows); ids of skipped
-- rows are simply unused. The bank category is resolved through bank_category_map (aliases first); the
-- caller upserts the categories first. Returns the inserted ids (the "new" rows).
WITH src AS (
  SELECT ir.*, row_number() OVER (ORDER BY ir.row_index) AS rn
  FROM import_rows ir
  WHERE ir.user_id = @user_id AND ir.import_id = @import_id AND ir.parse_error IS NULL
    AND ir.fingerprint NOT IN (SELECT d.fingerprint FROM dedupe_overrides d WHERE d.user_id = @user_id AND d.account_id = @account_id)
), ids AS (
  SELECT u.id, u.ord FROM unnest(@ids::uuid[]) WITH ORDINALITY AS u(id, ord)
)
INSERT INTO transactions (id, user_id, account_id, origin, fingerprint, occurrence, split_scope, source_import_id,
                          booked_on, value_on, amount_minor, currency, payee, payee_n, payee_group,
                          description, description_n, reference, reference_n, tx_type, tx_type_n,
                          bank_category, bank_category_n, counterparty_account, counterparty_n,
                          balance_after_minor, category_id, category_source, raw, search_text)
SELECT ids.id, src.user_id, @account_id, 'import', src.fingerprint, src.occurrence, src.split_scope, src.import_id,
       src.booked_on, src.value_on, src.amount_minor, src.currency, src.payee, src.payee_n, src.payee_group,
       src.description, src.description_n, src.reference, src.reference_n, src.tx_type, src.tx_type_n,
       src.bank_category, src.bank_category_n, src.counterparty_account, src.counterparty_n,
       src.balance_after_minor, m.category_id, CASE WHEN m.category_id IS NULL THEN 'none' ELSE 'bank' END, src.raw, src.search_text
FROM src
JOIN ids ON ids.ord = src.rn
LEFT JOIN bank_category_map m ON m.user_id = src.user_id AND m.name_n = src.bank_category_n AND src.bank_category_n <> ''
ORDER BY src.row_index
ON CONFLICT (account_id, fingerprint, occurrence, split_scope) DO NOTHING
RETURNING id;

-- name: ApplyAnnotations :execrows
-- Commit step 1b: re-apply user annotations (group, manual category, note, manual transfer mark)
-- to freshly inserted transactions, so labels survive delete + re-import.
UPDATE transactions t
SET group_id = a.group_id,
    category_id = coalesce(a.category_id, t.category_id),
    category_source = CASE WHEN a.category_id IS NOT NULL THEN 'manual' ELSE t.category_source END,
    note = a.note,
    note_n = a.note_n,
    is_transfer = coalesce(a.transfer_override, t.is_transfer),
    transfer_source = CASE WHEN a.transfer_override IS NOT NULL THEN 'manual' ELSE t.transfer_source END
FROM tx_annotations a
WHERE t.user_id = @user_id AND t.id = ANY(@ids::uuid[])
  AND a.user_id = @user_id
  AND (a.account_id, a.fingerprint, a.occurrence, a.split_scope) = (t.account_id, t.fingerprint, t.occurrence, t.split_scope);

-- name: LinkImportRows :execrows
-- Commit step 2: link every staged row to its transaction, new or pre-existing (provenance).
UPDATE import_rows ir SET transaction_id = t.id
FROM transactions t
WHERE ir.user_id = @user_id AND ir.import_id = @import_id AND ir.parse_error IS NULL
  AND t.user_id = @user_id AND t.account_id = @account_id
  AND t.fingerprint = ir.fingerprint AND t.occurrence = ir.occurrence AND t.split_scope = ir.split_scope;

-- name: DeleteOrphanTransactions :execrows
-- Delete-import step 2 (PLAN §4.4): import-origin transactions of the account that no staged row
-- references any more. Transactions another import also contains stay.
DELETE FROM transactions t
WHERE t.user_id = @user_id AND t.account_id = @account_id AND t.origin = 'import'
  AND NOT EXISTS (SELECT 1 FROM import_rows ir WHERE ir.transaction_id = t.id);

-- name: UpdateTransactionAnnotations :many
-- The user-editable fields of a set of transactions (ledger.Annotate). Each set_* flag says whether
-- that field is part of the patch. Resetting the category (set_category with a NULL category_id)
-- falls back to the bank category. Returns the dedupe identity and the new values, which the caller
-- mirrors into tx_annotations.
UPDATE transactions t
SET group_id = CASE WHEN @set_group::bool THEN sqlc.narg(group_id)::uuid ELSE t.group_id END,
    category_id = CASE WHEN NOT @set_category::bool THEN t.category_id
                       WHEN sqlc.narg(category_id)::uuid IS NOT NULL THEN sqlc.narg(category_id)::uuid
                       ELSE (SELECT m.category_id FROM bank_category_map m WHERE m.user_id = t.user_id AND m.name_n = t.bank_category_n AND t.bank_category_n <> '') END,
    category_source = CASE WHEN NOT @set_category::bool THEN t.category_source
                           WHEN sqlc.narg(category_id)::uuid IS NOT NULL THEN 'manual'
                           WHEN EXISTS (SELECT 1 FROM bank_category_map m WHERE m.user_id = t.user_id AND m.name_n = t.bank_category_n AND t.bank_category_n <> '' AND m.category_id IS NOT NULL) THEN 'bank'
                           ELSE 'none' END,
    note = CASE WHEN @set_note::bool THEN @note::text ELSE t.note END,
    note_n = CASE WHEN @set_note::bool THEN @note_n::text ELSE t.note_n END,
    updated_at = @now
WHERE t.user_id = @user_id AND t.id = ANY(@ids::uuid[])
RETURNING t.id, t.account_id, t.fingerprint, t.occurrence, t.split_scope, t.group_id, t.category_id, t.category_source, t.note, t.note_n;
