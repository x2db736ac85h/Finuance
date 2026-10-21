-- name: GetPreferences :one
SELECT * FROM user_preferences WHERE user_id = @user_id;

-- name: SavePreferences :one
-- Optimistic concurrency. expected_version is the version the client read: 0 means "there is no
-- document yet" and creates it at version 1; n > 0 updates only when the stored version is n and
-- bumps it. Any mismatch returns no row, which the caller reports as 409 stale_version.
WITH upd AS (
  UPDATE user_preferences p
  SET prefs = @prefs, version = p.version + 1, updated_at = @now
  WHERE p.user_id = @user_id AND p.version = @expected_version AND @expected_version::int > 0
  RETURNING p.version
), ins AS (
  INSERT INTO user_preferences (user_id, prefs, version, updated_at)
  SELECT @user_id, @prefs, 1, @now
  WHERE @expected_version::int = 0
  ON CONFLICT (user_id) DO NOTHING
  RETURNING version
)
SELECT upd.version FROM upd UNION ALL SELECT ins.version FROM ins;
