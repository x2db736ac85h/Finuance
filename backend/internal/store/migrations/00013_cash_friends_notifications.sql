-- Phase 19 (PR 2): cash accounts with typed expenses, friends, in-app notifications
-- (ADR 0027 friends, ADR 0029 manual transactions).
--
-- accounts.kind: a cash account takes expenses typed by its owner (transactions with
-- origin = 'manual', which 00002 already allowed); a bank account takes imported files. The kind is
-- chosen when the account is created.
--
-- friend_codes: one short-lived code per user, stored as its SHA-256 only. Like sessions it is a
-- bearer secret looked up by its hash before the other user is known, so it sits outside RLS and
-- only internal/friends reaches it (store.WithoutUser).
--
-- friendships: one row per pair, the smaller id first. It has no user_id: forced RLS lets either
-- side see, add (only as one of the two) and remove the row.
--
-- notifications: user-owned like every other table (forced RLS, the usual tenant policy). Another
-- user's action writes a notification under the recipient's own context.

-- +goose Up
ALTER TABLE accounts
  ADD COLUMN kind text NOT NULL DEFAULT 'bank' CHECK (kind IN ('bank', 'cash'));

CREATE TABLE friend_codes (
  user_id     uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  code_hash   bytea NOT NULL UNIQUE CHECK (octet_length(code_hash) = 32),
  expires_at  timestamptz NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX friend_codes_expires_idx ON friend_codes (expires_at);

CREATE TABLE friendships (
  user_a      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_b      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_a, user_b),
  CHECK (user_a < user_b)
);
CREATE INDEX friendships_user_b_idx ON friendships (user_b);
ALTER TABLE friendships ENABLE ROW LEVEL SECURITY;
ALTER TABLE friendships FORCE ROW LEVEL SECURITY;
CREATE POLICY friendships_sides ON friendships
  USING      (nullif(current_setting('app.user_id', true), '')::uuid IN (user_a, user_b))
  WITH CHECK (nullif(current_setting('app.user_id', true), '')::uuid IN (user_a, user_b));

CREATE TABLE notifications (
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind        text NOT NULL CHECK (kind ~ '^[a-z_]+\.[a-z_]+$'),
  data        jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object' AND octet_length(data::text) <= 2048),
  created_at  timestamptz NOT NULL DEFAULT now(),
  read_at     timestamptz
);
CREATE INDEX notifications_user_created_idx ON notifications (user_id, created_at DESC);
ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY notifications_tenant ON notifications
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE notifications;
DROP TABLE friendships;
DROP TABLE friend_codes;
ALTER TABLE accounts DROP COLUMN kind;
