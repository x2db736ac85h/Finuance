-- name: Ping :one
SELECT 1::int AS ok;

-- name: SchemaVersion :one
-- Highest applied goose migration (the readiness check compares it with the binary's expectation).
SELECT coalesce(max(version_id), 0)::bigint AS version
FROM goose_db_version
WHERE is_applied;
