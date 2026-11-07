-- Phase 20 (PR 3): assistant digests and the daily token budget (ADR 0033).
--
-- assistant_settings.digest*: the user's digest schedule (off by default), the time zone its hour
-- is in, and the connection and model the panel used when it was switched on.
-- users.digest_due_at: when the user's next digest is due (NULL: none). A time only, so the
-- worker's scan finds due users with one indexed query outside any user's context.
-- assistant_chats.kind: a digest is a chat of its own.
-- assistant_usage: tokens and runs per user and UTC day, added in the transaction that stores the
-- answer. User-owned, forced RLS.

-- +goose Up
ALTER TABLE assistant_settings
  ADD COLUMN digest            text NOT NULL DEFAULT 'off' CHECK (digest IN ('off', 'daily', 'weekly')),
  ADD COLUMN digest_hour       smallint NOT NULL DEFAULT 7 CHECK (digest_hour BETWEEN 0 AND 23),
  ADD COLUMN digest_weekday    smallint NOT NULL DEFAULT 1 CHECK (digest_weekday BETWEEN 1 AND 7), -- ISO: 1 = Monday
  ADD COLUMN digest_time_zone  text NOT NULL DEFAULT 'Europe/Warsaw' CHECK (char_length(digest_time_zone) BETWEEN 1 AND 64),
  ADD COLUMN digest_connection text NOT NULL DEFAULT '' CHECK (char_length(digest_connection) <= 40),
  ADD COLUMN digest_model      text NOT NULL DEFAULT '' CHECK (char_length(digest_model) <= 200);

ALTER TABLE users ADD COLUMN digest_due_at timestamptz;
CREATE INDEX users_digest_due_idx ON users (digest_due_at) WHERE digest_due_at IS NOT NULL;

ALTER TABLE assistant_chats ADD COLUMN kind text NOT NULL DEFAULT 'chat' CHECK (kind IN ('chat', 'digest'));

CREATE TABLE assistant_usage (
  user_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day      date NOT NULL,
  runs     integer NOT NULL DEFAULT 0 CHECK (runs >= 0),
  digests  integer NOT NULL DEFAULT 0 CHECK (digests >= 0),
  tokens   bigint NOT NULL DEFAULT 0 CHECK (tokens >= 0),
  PRIMARY KEY (user_id, day)
);
ALTER TABLE assistant_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_usage FORCE ROW LEVEL SECURITY;
CREATE POLICY assistant_usage_tenant ON assistant_usage
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE assistant_usage;
ALTER TABLE assistant_chats DROP COLUMN kind;
DROP INDEX users_digest_due_idx;
ALTER TABLE users DROP COLUMN digest_due_at;
ALTER TABLE assistant_settings
  DROP COLUMN digest, DROP COLUMN digest_hour, DROP COLUMN digest_weekday, DROP COLUMN digest_time_zone,
  DROP COLUMN digest_connection, DROP COLUMN digest_model;
