-- Duplicate review (PLAN §4.4, "Duplicate review and overrides"). ledger.Rematerialize uses these to
-- rebuild the transactions of one (account, fingerprint) from its staged rows and override.

-- name: GetDedupeOverride :one
SELECT * FROM dedupe_overrides WHERE user_id = @user_id AND account_id = @account_id AND fingerprint = @fingerprint;

-- name: FingerprintRows :many
-- The non-errored staged rows of one fingerprint in the account's committed imports, earliest
-- committed import first: the default value source is the first row of each occurrence here.
SELECT ir.import_id, ir.row_index, ir.occurrence, i.sha256
FROM import_rows ir
JOIN imports i ON i.id = ir.import_id
WHERE ir.user_id = @user_id AND i.user_id = @user_id AND i.account_id = @account_id AND i.status = 'committed'
  AND ir.parse_error IS NULL AND ir.fingerprint = @fingerprint
ORDER BY i.committed_at, i.id, ir.row_index;

-- name: FingerprintTransactions :many
SELECT id, occurrence, split_scope, source_import_id
FROM transactions
WHERE user_id = @user_id AND account_id = @account_id AND fingerprint = @fingerprint AND origin = 'import';

-- name: DeleteTransactionsByID :execrows
DELETE FROM transactions WHERE user_id = @user_id AND id = ANY(@ids::uuid[]);

-- name: InsertTransactionFromRow :exec
-- One transaction from one staged row (its value source). The bank category resolves as in the
-- commit; annotations are re-applied by the caller.
INSERT INTO transactions (id, user_id, account_id, origin, fingerprint, occurrence, split_scope, source_import_id,
                          booked_on, value_on, amount_minor, currency, payee, payee_n, payee_group,
                          description, description_n, reference, reference_n, tx_type, tx_type_n,
                          bank_category, bank_category_n, counterparty_account, counterparty_n,
                          balance_after_minor, category_id, category_source, raw, search_text)
SELECT @id, ir.user_id, @account_id, 'import', ir.fingerprint, ir.occurrence, @split_scope, ir.import_id,
       ir.booked_on, ir.value_on, ir.amount_minor, ir.currency, ir.payee, ir.payee_n, ir.payee_group,
       ir.description, ir.description_n, ir.reference, ir.reference_n, ir.tx_type, ir.tx_type_n,
       ir.bank_category, ir.bank_category_n, ir.counterparty_account, ir.counterparty_n,
       ir.balance_after_minor, c.id, CASE WHEN c.id IS NULL THEN 'none' ELSE 'bank' END, ir.raw, ir.search_text
FROM import_rows ir
LEFT JOIN categories c ON c.user_id = ir.user_id AND c.name_n = ir.bank_category_n AND ir.bank_category_n <> ''
WHERE ir.user_id = @user_id AND ir.import_id = @import_id AND ir.row_index = @row_index;

-- name: UpdateTransactionFromRow :exec
-- Re-point a kept transaction at a (possibly new) value source: every imported field comes from the
-- row. User edits stay: a manual or rule category is kept, the bank category is re-resolved.
UPDATE transactions t
SET source_import_id = ir.import_id,
    booked_on = ir.booked_on, value_on = ir.value_on, amount_minor = ir.amount_minor, currency = ir.currency,
    payee = ir.payee, payee_n = ir.payee_n, payee_group = ir.payee_group,
    description = ir.description, description_n = ir.description_n, reference = ir.reference, reference_n = ir.reference_n,
    tx_type = ir.tx_type, tx_type_n = ir.tx_type_n, bank_category = ir.bank_category, bank_category_n = ir.bank_category_n,
    counterparty_account = ir.counterparty_account, counterparty_n = ir.counterparty_n,
    balance_after_minor = ir.balance_after_minor, raw = ir.raw, search_text = ir.search_text,
    category_id = CASE WHEN t.category_source IN ('none', 'bank') THEN c.id ELSE t.category_id END,
    category_source = CASE WHEN t.category_source NOT IN ('none', 'bank') THEN t.category_source
                           WHEN c.id IS NULL THEN 'none' ELSE 'bank' END,
    updated_at = @now
FROM import_rows ir
LEFT JOIN categories c ON c.user_id = ir.user_id AND c.name_n = ir.bank_category_n AND ir.bank_category_n <> ''
WHERE t.user_id = @user_id AND t.id = @id
  AND ir.user_id = @user_id AND ir.import_id = @import_id AND ir.row_index = @row_index;

-- name: LinkImportRow :exec
UPDATE import_rows SET split_scope = @split_scope, transaction_id = @transaction_id
WHERE user_id = @user_id AND import_id = @import_id AND row_index = @row_index;

-- name: ImportBankCategories :many
-- Distinct bank categories of an import's valid rows (the commit upserts them as categories).
SELECT DISTINCT bank_category FROM import_rows
WHERE user_id = @user_id AND import_id = @import_id AND parse_error IS NULL AND bank_category <> ''
ORDER BY bank_category;

-- name: OverriddenFingerprintsInImport :many
-- Fingerprints of this import that have an override: the commit re-materialises them (step 2b).
SELECT DISTINCT ir.fingerprint::bytea AS fingerprint
FROM import_rows ir
JOIN dedupe_overrides d ON d.user_id = ir.user_id AND d.account_id = @account_id AND d.fingerprint = ir.fingerprint
WHERE ir.user_id = @user_id AND ir.import_id = @import_id AND ir.parse_error IS NULL;

-- name: FingerprintsSourcedLater :many
-- Fingerprints of this import whose transaction takes its values from an import committed after
-- this one (or from none). After a reprocess this import may be the earliest source again; these
-- fingerprints are re-materialised so the default rule ("earliest import wins") holds.
SELECT DISTINCT t.fingerprint
FROM import_rows ir
JOIN transactions t ON t.id = ir.transaction_id
JOIN imports me ON me.id = ir.import_id
LEFT JOIN imports src ON src.id = t.source_import_id
WHERE ir.user_id = @user_id AND ir.import_id = @import_id AND t.user_id = @user_id
  AND (src.id IS NULL OR (src.id <> me.id AND (src.committed_at, src.id) > (me.committed_at, me.id)));

-- name: FingerprintsSourcedBy :many
-- Fingerprints whose transactions take their values from this import (delete / reprocess must move
-- them to another source).
SELECT DISTINCT fingerprint FROM transactions
WHERE user_id = @user_id AND account_id = @account_id AND source_import_id = @import_id;

-- name: OverridesPreferring :many
-- Overrides that prefer this import: deleting the import removes them (FK cascade), so their
-- fingerprints are re-materialised.
SELECT fingerprint FROM dedupe_overrides WHERE user_id = @user_id AND account_id = @account_id AND preferred_import_id = @import_id;

-- name: DuplicatesSkipped :one
-- "N duplicates skipped" for a set of accounts: linked staged rows − import-origin transactions.
SELECT ((SELECT count(*) FROM import_rows ir JOIN imports i ON i.id = ir.import_id
        WHERE ir.user_id = @user_id AND i.account_id = ANY(@account_ids::uuid[]) AND ir.transaction_id IS NOT NULL)
     - (SELECT count(*) FROM transactions t
        WHERE t.user_id = @user_id AND t.account_id = ANY(@account_ids::uuid[]) AND t.origin = 'import'))::bigint AS skipped;
