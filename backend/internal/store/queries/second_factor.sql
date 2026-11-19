-- Two-step sign-in (migration 00027, ADR 0040). The tables are outside RLS like sessions; every
-- statement names the user or the hash of a secret token only its holder has.

-- name: GetSecondFactor :one
SELECT user_id, secret_sealed, created_at, confirmed_at, last_step
FROM user_second_factors WHERE user_id = @user_id;

-- name: LockSecondFactor :one
-- The row locked for the check of a code, so two requests can't both use one code.
SELECT user_id, secret_sealed, created_at, confirmed_at, last_step
FROM user_second_factors WHERE user_id = @user_id FOR UPDATE;

-- name: HasConfirmedSecondFactor :one
SELECT EXISTS (SELECT 1 FROM user_second_factors WHERE user_id = @user_id AND confirmed_at IS NOT NULL);

-- name: StartSecondFactor :exec
-- Starts (or restarts) enrolment. A confirmed factor is never replaced this way: it has to be
-- switched off first, with the password and a code.
INSERT INTO user_second_factors (user_id, secret_sealed, created_at)
VALUES (@user_id, @secret_sealed, @now)
ON CONFLICT (user_id) DO UPDATE
  SET secret_sealed = EXCLUDED.secret_sealed, created_at = EXCLUDED.created_at, last_step = 0
  WHERE user_second_factors.confirmed_at IS NULL;

-- name: ConfirmSecondFactor :execrows
UPDATE user_second_factors SET confirmed_at = @now, last_step = @last_step
WHERE user_id = @user_id AND confirmed_at IS NULL;

-- name: SetSecondFactorStep :exec
UPDATE user_second_factors SET last_step = @last_step WHERE user_id = @user_id AND last_step < @last_step;

-- name: DeleteSecondFactor :execrows
DELETE FROM user_second_factors WHERE user_id = @user_id;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = @user_id;

-- name: CreateRecoveryCode :exec
INSERT INTO recovery_codes (code_hash, user_id, created_at) VALUES (@code_hash, @user_id, @now);

-- name: UseRecoveryCode :execrows
-- Marks one of the user's unused codes as used; 0 rows: not theirs, unknown or used before.
UPDATE recovery_codes SET used_at = @now
WHERE code_hash = @code_hash AND user_id = @user_id AND used_at IS NULL;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM recovery_codes WHERE user_id = @user_id AND used_at IS NULL;

-- name: CreateLoginChallenge :exec
INSERT INTO login_challenges (token_hash, user_id, created_at, expires_at)
VALUES (@token_hash, @user_id, @now, @expires_at);

-- name: DeleteUserLoginChallenges :exec
-- A new sign-in replaces the user's earlier challenges; a completed one removes them all.
DELETE FROM login_challenges WHERE user_id = @user_id;

-- name: TryLoginChallenge :one
-- Counts one attempt against a live challenge and returns it; no row: unknown, expired or used up.
UPDATE login_challenges SET attempts = attempts + 1
WHERE token_hash = @token_hash AND expires_at > @now AND attempts < @max_attempts
RETURNING user_id, attempts;

-- name: DeleteExpiredLoginChallenges :execrows
DELETE FROM login_challenges WHERE expires_at <= @now;

-- name: UsersWithSecondFactor :many
-- Which of the given users have two-step sign-in on (the administration's user list).
SELECT user_id FROM user_second_factors WHERE confirmed_at IS NOT NULL AND user_id = ANY(@user_ids::uuid[]);

-- name: ListAdminsWithoutSecondFactor :many
-- Administrators (not disabled) without a confirmed second factor; for the settings page's warning.
SELECT u.id, u.email FROM users u
WHERE u.is_admin AND u.disabled_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM user_second_factors f WHERE f.user_id = u.id AND f.confirmed_at IS NOT NULL)
ORDER BY u.email;

-- name: GetLoginChallengeUser :one
-- Whose live challenge this is, without counting an attempt.
SELECT user_id FROM login_challenges
WHERE token_hash = @token_hash AND expires_at > @now AND attempts < @max_attempts;
