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
       ir.balance_after_minor, m.category_id, CASE WHEN m.category_id IS NULL THEN 'none' ELSE 'bank' END, ir.raw, ir.search_text
FROM import_rows ir
LEFT JOIN bank_category_map m ON m.user_id = ir.user_id AND m.name_n = ir.bank_category_n AND ir.bank_category_n <> ''
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
    category_id = CASE WHEN t.category_source IN ('none', 'bank') THEN m.category_id ELSE t.category_id END,
    category_source = CASE WHEN t.category_source NOT IN ('none', 'bank') THEN t.category_source
                           WHEN m.category_id IS NULL THEN 'none' ELSE 'bank' END,
    updated_at = @now
FROM import_rows ir
LEFT JOIN bank_category_map m ON m.user_id = ir.user_id AND m.name_n = ir.bank_category_n AND ir.bank_category_n <> ''
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

-- name: DuplicateGroupKeys :many
-- The duplicate groups of the given accounts (PLAN §7.14, legacy §32.3): fingerprints with staged
-- rows in two or more committed imports where something was merged (more linked rows than
-- transactions) or the user decided (an override). Every token must appear in one of the rows'
-- search text. Newest first; `total` counts every group that matches.
WITH fp AS (
  SELECT i.account_id, ir.fingerprint,
         count(DISTINCT ir.import_id) AS imports,
         count(ir.transaction_id) AS linked,
         count(DISTINCT ir.transaction_id) AS txs,
         max(ir.booked_on) AS last_on,
         bool_or(NOT EXISTS (SELECT 1 FROM unnest(@tokens::text[]) tok WHERE strpos(coalesce(ir.search_text, ''), tok) = 0)) AS hit
  FROM import_rows ir
  JOIN imports i ON i.id = ir.import_id
  WHERE ir.user_id = @user_id AND i.user_id = @user_id AND i.status = 'committed' AND ir.parse_error IS NULL
    AND ir.fingerprint IS NOT NULL AND i.account_id = ANY(@account_ids::uuid[])
  GROUP BY i.account_id, ir.fingerprint
)
SELECT fp.account_id::uuid AS account_id, fp.fingerprint::bytea AS fingerprint, count(*) OVER ()::bigint AS total
FROM fp
LEFT JOIN dedupe_overrides d ON d.user_id = @user_id AND d.account_id = fp.account_id AND d.fingerprint = fp.fingerprint
WHERE fp.imports >= 2 AND (fp.linked > fp.txs OR d.fingerprint IS NOT NULL) AND fp.hit
ORDER BY fp.last_on DESC NULLS LAST, fp.account_id, fp.fingerprint
OFFSET @row_offset LIMIT @row_limit;

-- name: DuplicateGroupRows :many
-- Every version of the given (account, fingerprint) pairs: the staged rows of committed imports
-- with their file, and whether each row is the one the data shows (its transaction's source).
-- pair_keys are "<account id>:<hex fingerprint>"; the ANY filters narrow the scan first.
SELECT i.account_id::uuid AS account_id, ir.fingerprint::bytea AS fingerprint, ir.import_id, i.filename, i.committed_at,
       ir.row_index, ir.occurrence, ir.booked_on, ir.value_on, ir.amount_minor, ir.currency, ir.balance_after_minor,
       ir.payee, ir.description, ir.reference, ir.tx_type, ir.bank_category, ir.counterparty_account, ir.raw,
       (t.id IS NOT NULL AND t.source_import_id = ir.import_id)::boolean AS in_data
FROM import_rows ir
JOIN imports i ON i.id = ir.import_id
LEFT JOIN transactions t ON t.id = ir.transaction_id AND t.user_id = @user_id
WHERE ir.user_id = @user_id AND i.user_id = @user_id AND i.status = 'committed' AND ir.parse_error IS NULL
  AND ir.fingerprint = ANY(@fingerprints::bytea[]) AND i.account_id = ANY(@account_ids::uuid[])
  AND (i.account_id::text || ':' || encode(ir.fingerprint, 'hex')) = ANY(@pair_keys::text[])
ORDER BY i.committed_at, i.id, ir.row_index;

-- name: ListDedupeOverridesFor :many
-- The overrides of the given (account, fingerprint) pairs.
SELECT d.* FROM dedupe_overrides d
WHERE d.user_id = @user_id AND d.fingerprint = ANY(@fingerprints::bytea[])
  AND (d.account_id::text || ':' || encode(d.fingerprint, 'hex')) = ANY(@pair_keys::text[]);

-- name: FingerprintsWithRows :many
-- The fingerprints, among the given ones, that still have rows in a committed import of the
-- account (delete-import: the others have nothing left to rebuild).
SELECT DISTINCT ir.fingerprint::bytea AS fingerprint
FROM import_rows ir
JOIN imports i ON i.id = ir.import_id
WHERE ir.user_id = @user_id AND i.user_id = @user_id AND i.account_id = @account_id AND i.status = 'committed'
  AND ir.parse_error IS NULL AND ir.fingerprint = ANY(@fingerprints::bytea[]);
