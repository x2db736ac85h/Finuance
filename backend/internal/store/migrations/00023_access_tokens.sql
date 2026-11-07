-- Phase 20 (PR 4): personal access tokens for the fin CLI and scripts (ADR 0036). Like sessions,
-- the table is outside RLS: a token is resolved before its user is known. Only the SHA-256 of a
-- token is stored, with its first 12 characters (fin_ + 8) to recognise it in the list. A token
-- reads (GET and a few read-only POSTs) or reads and writes; it never reaches the admin, sign-in,
-- password, session or token routes. At most a year; revoked tokens stay listed for 30 days.

-- +goose Up
CREATE TABLE access_tokens (
  id            uuid PRIMARY KEY,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name          text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
  token_hash    bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
  prefix        text NOT NULL CHECK (char_length(prefix) = 12),
  scope         text NOT NULL CHECK (scope IN ('read', 'write')),
  created_at    timestamptz NOT NULL,
  expires_at    timestamptz NOT NULL,
  last_used_at  timestamptz,
  revoked_at    timestamptz,
  CHECK (expires_at > created_at AND expires_at <= created_at + interval '366 days')
);
CREATE INDEX access_tokens_user_idx ON access_tokens (user_id, created_at DESC);

-- +goose Down
DROP TABLE access_tokens;
