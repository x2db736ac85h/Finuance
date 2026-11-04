-- Manual transactions (Phase 19, ADR 0029): expenses typed into a cash account. They have
-- origin = 'manual' and a random fingerprint, so no import ever matches them; re-materialisation,
-- import deletes and the duplicate counts only touch origin = 'import' rows.

-- name: InsertManualTransaction :exec
INSERT INTO transactions (id, user_id, account_id, origin, fingerprint, occurrence, booked_on, amount_minor, currency,
                          payee, payee_n, payee_group, description, description_n, search_text, created_at, updated_at)
VALUES (@id, @user_id, @account_id, 'manual', @fingerprint, 1, @booked_on, @amount_minor, @currency,
        @payee, @payee_n, @payee_group, @description, @description_n, @search_text, @now, @now);

-- name: UpdateManualTransaction :execrows
-- The typed fields of a manual row; imported rows are never matched (origin), so their bank
-- values stay as the file had them.
UPDATE transactions
SET booked_on = @booked_on, amount_minor = @amount_minor, payee = @payee, payee_n = @payee_n,
    payee_group = @payee_group, description = @description, description_n = @description_n,
    search_text = @search_text, updated_at = @now
WHERE user_id = @user_id AND id = @id AND origin = 'manual';

-- name: DeleteManualTransaction :one
-- Deletes a manual row and returns its identity, so its annotation goes too.
DELETE FROM transactions
WHERE user_id = @user_id AND id = @id AND origin = 'manual'
RETURNING account_id, fingerprint, occurrence, split_scope;

-- name: DeleteAnnotation :exec
DELETE FROM tx_annotations
WHERE user_id = @user_id AND account_id = @account_id AND fingerprint = @fingerprint
  AND occurrence = @occurrence AND split_scope = @split_scope;
