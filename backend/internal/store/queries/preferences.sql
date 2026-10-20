-- name: GetPreferences :one
SELECT * FROM user_preferences WHERE user_id = @user_id;

-- name: SavePreferences :one
-- Optimistic concurrency: the update applies only when the stored version is the one the client
-- read (expected_version); otherwise no row is returned and the caller reports a conflict. The first
-- save creates the row at version 1.
INSERT INTO user_preferences AS p (user_id, prefs, version, updated_at)
VALUES (@user_id, @prefs, 1, @now)
ON CONFLICT (user_id) DO UPDATE
SET prefs = excluded.prefs, version = p.version + 1, updated_at = excluded.updated_at
WHERE p.version = @expected_version
RETURNING version;
