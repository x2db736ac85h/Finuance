-- Sessions store only the SHA-256 of the cookie token (PLAN §5.2). expires_at is the absolute
-- expiry (created + FINUANCE_SESSION_TTL); the idle expiry is last_seen_at + FINUANCE_SESSION_IDLE_TTL,
-- passed in as idle_cutoff. Time comes from the caller (platform.Clock), so both are testable.

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, created_at, last_seen_at, expires_at, user_agent, ip)
VALUES (@token_hash, @user_id, @now, @now, @expires_at, @user_agent, sqlc.narg(ip));

-- name: GetSession :one
-- A live session and its user: not past the absolute expiry and seen after idle_cutoff.
SELECT s.token_hash, s.user_id, s.created_at, s.last_seen_at, s.expires_at,
       u.email, u.display_name, u.is_demo, u.created_at AS user_created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash AND s.expires_at > @now AND s.last_seen_at > @idle_cutoff;

-- name: TouchSession :exec
-- Records activity (idle expiry). The caller throttles it to once every few minutes.
UPDATE sessions SET last_seen_at = @now WHERE token_hash = @token_hash;

-- name: ListUserSessions :many
SELECT token_hash, created_at, last_seen_at, expires_at, user_agent, ip
FROM sessions WHERE user_id = @user_id AND expires_at > @now AND last_seen_at > @idle_cutoff
ORDER BY last_seen_at DESC;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = @token_hash;

-- name: DeleteUserSessionByPrefix :execrows
-- Revoke one session by its public id: the first 8 bytes of the token hash (PLAN §6.2).
DELETE FROM sessions WHERE user_id = @user_id AND substring(token_hash FROM 1 FOR 8) = @prefix::bytea;

-- name: DeleteUserSessions :execrows
-- Log out everywhere, optionally keeping one session (pass an empty keep_token_hash to keep none).
DELETE FROM sessions WHERE user_id = @user_id AND token_hash <> @keep_token_hash;

-- name: DeleteExpiredSessions :execrows
-- Periodic cleanup: absolute expiry passed, or idle for longer than the idle timeout.
DELETE FROM sessions WHERE expires_at <= @now OR last_seen_at <= @idle_cutoff;
