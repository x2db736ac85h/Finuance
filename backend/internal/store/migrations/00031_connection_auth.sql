-- Phase 26 (26.2, 26.3; ADR 0045): how a stored connection authenticates.
--   auth      'key' (a sealed key, or none: everything until now) or 'google' (short-lived tokens
--             from Google's credentials; no key is stored)
--   project   the Google Cloud project of a 'google' connection
--   location  its Vertex AI location, always chosen ('europe-west4', 'eu', 'global')
--   caps      the administrator's corrections of what is detected about the connection's models
--             (llm.CapsOverride as JSON): a hosted endpoint reports nothing about them
-- A Google connection is a cloud connection without a key; the table refuses anything else.

-- +goose Up
ALTER TABLE assistant_connections
  ADD COLUMN auth text NOT NULL DEFAULT 'key',
  ADD COLUMN project text NOT NULL DEFAULT '',
  ADD COLUMN location text NOT NULL DEFAULT '',
  ADD COLUMN caps jsonb NOT NULL DEFAULT '[]',
  ADD CONSTRAINT assistant_connections_auth_check CHECK (auth IN ('key', 'google')),
  ADD CONSTRAINT assistant_connections_google_check CHECK (
    (auth = 'google' AND cloud AND api_key IS NULL AND api = 'openai'
       AND project ~ '^[a-z][a-z0-9-]{4,28}[a-z0-9]$' AND location ~ '^[a-z0-9-]{2,40}$')
    OR (auth = 'key' AND project = '' AND location = '')),
  ADD CONSTRAINT assistant_connections_caps_check CHECK (jsonb_typeof(caps) = 'array' AND pg_column_size(caps) <= 16384);

-- +goose Down
ALTER TABLE assistant_connections
  DROP CONSTRAINT assistant_connections_caps_check,
  DROP CONSTRAINT assistant_connections_google_check,
  DROP CONSTRAINT assistant_connections_auth_check,
  DROP COLUMN caps,
  DROP COLUMN location,
  DROP COLUMN project,
  DROP COLUMN auth;
