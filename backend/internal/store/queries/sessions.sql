-- Sessions store only the SHA-256 of the cookie token (PLAN §5.2). Time comes from the caller
-- (platform.Clock), so expiry is testable.

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, created_at, last_seen_at, expires_at, user_agent, ip)
VALUES (@token_hash, @user_id, @now, @now, @expires_at, @user_agent, sqlc.narg(ip));

-- name: GetSession :one
-- A live session and its user; expired sessions are not returned.
SELECT s.token_hash, s.user_id, s.created_at, s.last_seen_at, s.expires_at, u.email, u.display_name, u.is_demo
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash AND s.expires_at > @now;

-- name: TouchSession :exec
-- Sliding expiry.
UPDATE sessions SET last_seen_at = @now, expires_at = @expires_at WHERE token_hash = @token_hash;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = @token_hash;

-- name: DeleteUserSessions :execrows
-- Log out everywhere (password change), optionally keeping the current session.
DELETE FROM sessions WHERE user_id = @user_id AND token_hash <> @keep_token_hash;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= @now;
