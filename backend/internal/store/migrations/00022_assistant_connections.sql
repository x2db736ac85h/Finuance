-- Phase 20 (PR 3): Administration → Assistant (ADR 0035). Instance tables, outside RLS like themes.
--
-- assistant_connections: model servers an administrator added in the panel, next to the read-only
-- ones of the connections file (whose names they can't take). api_key is sealed with AES-256-GCM
-- under FINUANCE_INSTANCE_KEY, bound to the connection's name; never returned by the API.
-- assistant_model_settings: per-model tuning for any connection (file or panel): a default
-- temperature, context size, thinking level and tool set, and a short note added to the prompt.

-- +goose Up
CREATE TABLE assistant_connections (
  name            text PRIMARY KEY CHECK (name ~ '^[a-z0-9][a-z0-9_-]{0,39}$'),
  label           text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 80),
  api             text NOT NULL CHECK (api IN ('openai', 'anthropic')),
  base_url        text NOT NULL CHECK (char_length(base_url) <= 500),
  cloud           boolean NOT NULL DEFAULT false,
  default_model   text NOT NULL DEFAULT '' CHECK (char_length(default_model) <= 200),
  models          text[] NOT NULL DEFAULT '{}' CHECK (cardinality(models) <= 50),
  context_tokens  integer NOT NULL DEFAULT 0 CHECK (context_tokens BETWEEN 0 AND 10000000),
  api_key         bytea CHECK (octet_length(api_key) <= 2048),
  version         integer NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL
);

CREATE TABLE assistant_model_settings (
  connection      text NOT NULL CHECK (char_length(connection) BETWEEN 1 AND 40),
  model           text NOT NULL CHECK (char_length(model) BETWEEN 1 AND 200),
  temperature     real CHECK (temperature BETWEEN 0 AND 2),
  context_tokens  integer CHECK (context_tokens BETWEEN 1024 AND 10000000),
  think           text CHECK (think IN ('off', 'auto', 'low', 'medium', 'high')),
  toolset         text CHECK (toolset IN ('full', 'compact')),
  extra_prompt    text NOT NULL DEFAULT '' CHECK (char_length(extra_prompt) <= 1000),
  updated_at      timestamptz NOT NULL,
  PRIMARY KEY (connection, model)
);

-- +goose Down
DROP TABLE assistant_model_settings;
DROP TABLE assistant_connections;
