-- Phase 23 (task 23.7): two-step sign-in with time-based one-time codes (ADR 0040). Like sessions,
-- the three tables are outside RLS: the second step runs before a session exists. Every query
-- names the user.
--   user_second_factors  one row per user: the TOTP secret sealed with the instance key (the user
--                        id is part of the seal, so a row copied to another user doesn't open),
--                        when it was confirmed (NULL: enrolment started, not yet in force), and the
--                        last time step a code was accepted for (a code can't be used twice).
--   recovery_codes       ten per user, as SHA-256 hashes; each works once.
--   login_challenges     the state between the password and the code: the hash of a random token
--                        the browser holds for a few minutes, with a small number of attempts.

-- +goose Up
CREATE TABLE user_second_factors (
  user_id        uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  secret_sealed  bytea NOT NULL CHECK (octet_length(secret_sealed) BETWEEN 32 AND 256),
  created_at     timestamptz NOT NULL,
  confirmed_at   timestamptz,
  last_step      bigint NOT NULL DEFAULT 0 CHECK (last_step >= 0)
);

CREATE TABLE recovery_codes (
  code_hash   bytea PRIMARY KEY CHECK (octet_length(code_hash) = 32),
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  timestamptz NOT NULL,
  used_at     timestamptz
);
CREATE INDEX recovery_codes_user_idx ON recovery_codes (user_id);

CREATE TABLE login_challenges (
  token_hash  bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  timestamptz NOT NULL,
  expires_at  timestamptz NOT NULL,
  attempts    integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  CHECK (expires_at > created_at AND expires_at <= created_at + interval '15 minutes')
);
CREATE INDEX login_challenges_user_idx ON login_challenges (user_id);
CREATE INDEX login_challenges_expires_idx ON login_challenges (expires_at);

-- +goose Down
DROP TABLE login_challenges;
DROP TABLE recovery_codes;
DROP TABLE user_second_factors;
