-- Administration → Assistant (ADR 0035; migration 00022): stored connections and per-model tuning.
-- Instance tables, read and written outside any user's context.

-- name: ListStoredConnections :many
SELECT * FROM assistant_connections ORDER BY name;

-- name: GetStoredConnection :one
SELECT * FROM assistant_connections WHERE name = @name;

-- name: InsertStoredConnection :one
INSERT INTO assistant_connections (name, label, api, base_url, cloud, default_model, models, context_tokens, api_key, created_at, updated_at)
VALUES (@name, @label, @api, @base_url, @cloud, @default_model, @models, @context_tokens, @api_key, @now, @now)
RETURNING *;

-- name: UpdateStoredConnection :one
-- Versioned: no row when the version read is no longer the stored one.
UPDATE assistant_connections
SET label = @label, api = @api, base_url = @base_url, cloud = @cloud, default_model = @default_model, models = @models,
    context_tokens = @context_tokens, api_key = @api_key, version = version + 1, updated_at = @now
WHERE name = @name AND version = @version
RETURNING *;

-- name: DeleteStoredConnection :execrows
DELETE FROM assistant_connections WHERE name = @name;

-- name: ListModelSettings :many
SELECT * FROM assistant_model_settings ORDER BY connection, model;

-- name: UpsertModelSettings :one
INSERT INTO assistant_model_settings (connection, model, temperature, context_tokens, think, toolset, extra_prompt, updated_at)
VALUES (@connection, @model, sqlc.narg(temperature), sqlc.narg(context_tokens), sqlc.narg(think), sqlc.narg(toolset), @extra_prompt, @now)
ON CONFLICT (connection, model) DO UPDATE
SET temperature = excluded.temperature, context_tokens = excluded.context_tokens, think = excluded.think,
    toolset = excluded.toolset, extra_prompt = excluded.extra_prompt, updated_at = excluded.updated_at
RETURNING *;

-- name: DeleteModelSettings :execrows
DELETE FROM assistant_model_settings WHERE connection = @connection AND model = @model;
