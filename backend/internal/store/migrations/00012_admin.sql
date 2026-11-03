-- Phase 19 (PR 1): instance administration and colour themes (ADR 0025, ADR 0026).
--
-- Everything here is instance-level, like users and sessions, and sits outside row-level
-- security: the auth and admin packages reach it through store.WithoutUser. None of it holds
-- financial data, and nothing here lets an administrator read any (ADR 0025).
--
-- users gains the administrator flag, a disabled state, a "must change password" flag for
-- one-time passwords, and per-user limit overrides (a JSON object; a missing key means the
-- instance default from instance_settings).

-- +goose Up
ALTER TABLE users
  ADD COLUMN is_admin             boolean NOT NULL DEFAULT false,
  ADD COLUMN disabled_at          timestamptz,
  ADD COLUMN must_change_password boolean NOT NULL DEFAULT false,
  ADD COLUMN limits               jsonb NOT NULL DEFAULT '{}'
    CHECK (jsonb_typeof(limits) = 'object' AND octet_length(limits::text) <= 1024);

-- One row: the instance's defaults for new users' views, the feature switches and the default
-- limits. Versioned like preferences, so two administrators can't overwrite each other unseen.
CREATE TABLE instance_settings (
  id          smallint PRIMARY KEY CHECK (id = 1),
  version     integer NOT NULL DEFAULT 0 CHECK (version >= 0),
  doc         jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(doc) = 'object' AND octet_length(doc::text) <= 16384),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX instance_settings_updated_by_idx ON instance_settings (updated_by);
INSERT INTO instance_settings (id) VALUES (1);

-- Colour themes made by administrators. The four built-in themes are code (web/src/styles);
-- rows here are drafts, approved themes (offered to users) and retired ones. Tokens are only
-- colours (validated by the admin service: known keys, #rrggbb[aa]), never CSS.
CREATE TABLE themes (
  id          uuid PRIMARY KEY,
  slug        text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
  name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
  based_on    text NOT NULL CHECK (based_on ~ '^[a-z0-9-]{1,32}$'),
  tokens      jsonb NOT NULL CHECK (jsonb_typeof(tokens) = 'object' AND octet_length(tokens::text) <= 8192),
  status      text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'approved', 'retired')),
  version     integer NOT NULL DEFAULT 0 CHECK (version >= 0),
  created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX themes_created_by_idx ON themes (created_by);

-- +goose Down
DROP TABLE themes;
DROP TABLE instance_settings;
ALTER TABLE users
  DROP COLUMN limits,
  DROP COLUMN must_change_password,
  DROP COLUMN disabled_at,
  DROP COLUMN is_admin;
