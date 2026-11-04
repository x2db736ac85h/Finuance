-- Friends (Phase 19, 19.7, ADR 0027).
--
-- friend_codes is outside RLS (like sessions): a code is a bearer secret looked up by its SHA-256
-- before the other user is known, reached only by internal/friends through store.WithoutUser.
-- friendships has forced RLS: either side sees, adds and removes the pair (user_a < user_b).

-- name: SetFriendCode :exec
-- One code per user: a new one replaces the previous.
INSERT INTO friend_codes (user_id, code_hash, expires_at, created_at)
VALUES (@user_id, @code_hash, @expires_at, @now)
ON CONFLICT (user_id) DO UPDATE SET code_hash = excluded.code_hash, expires_at = excluded.expires_at, created_at = excluded.created_at;

-- name: FindFriendCode :one
SELECT user_id, expires_at FROM friend_codes WHERE code_hash = @code_hash;

-- name: DeleteFriendCode :exec
DELETE FROM friend_codes WHERE user_id = @user_id;

-- name: DeleteExpiredFriendCodes :execrows
DELETE FROM friend_codes WHERE expires_at < @now;

-- name: FriendDisplayName :one
-- The display name of a user (friends never see each other's e-mail).
SELECT display_name FROM users WHERE id = @id AND disabled_at IS NULL;

-- name: AddFriendship :execrows
INSERT INTO friendships (user_a, user_b, created_at) VALUES (@user_a, @user_b, @now)
ON CONFLICT DO NOTHING;

-- name: ListFriends :many
-- The user's friends with their display names, newest first.
SELECT u.id, u.display_name, f.created_at
FROM friendships f
JOIN users u ON u.id = CASE WHEN f.user_a = @user_id THEN f.user_b ELSE f.user_a END
WHERE f.user_a = @user_id OR f.user_b = @user_id
ORDER BY f.created_at DESC, u.id;

-- name: DeleteFriendship :execrows
DELETE FROM friendships WHERE user_a = @user_a AND user_b = @user_b;
