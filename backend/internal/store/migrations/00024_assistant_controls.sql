-- Phase 20 follow-ups (20.17–20.20): an administrator's controls for each assistant connection, the
-- connections file's included (ADR 0035 addendum). An instance table, outside RLS like
-- assistant_connections. A connection without a row is on, unloads its model after 30 idle minutes
-- where the server supports it, and uses its own default model.
--
-- enabled: off hides the connection from everyone and its chats and digests use the default one.
-- unload_after_minutes: how long the server keeps a model loaded after the last request; NULL leaves
-- it to the server's own setting.
-- default_model, photo_model: the model questions use (overriding the connection's own default) and
-- the one a question with photos uses when that model can't read images; '' = none.

-- +goose Up
CREATE TABLE assistant_connection_controls (
  name                  text PRIMARY KEY CHECK (name ~ '^[a-z0-9][a-z0-9_-]{0,39}$'),
  enabled               boolean NOT NULL DEFAULT true,
  unload_after_minutes  integer CHECK (unload_after_minutes IN (10, 15, 30, 45, 60, 120, 240)),
  default_model         text NOT NULL DEFAULT '' CHECK (char_length(default_model) <= 200),
  photo_model           text NOT NULL DEFAULT '' CHECK (char_length(photo_model) <= 200),
  version               integer NOT NULL DEFAULT 1,
  updated_at            timestamptz NOT NULL
);

-- +goose Down
DROP TABLE assistant_connection_controls;
