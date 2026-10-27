-- Phase 17: the audit log (PLAN task 17.6). One row per security-relevant action of a user
-- (sign-ins, exports, deletions, …), shown to that user on Settings → Activity and nowhere else.
--
-- Append-only for the app: finuance_app may read and insert its own rows, never update them, and
-- may delete only rows older than the retention (365 days, our Product Engineer's decision), which
-- the daily cleanup job does. Recent history can't be erased through the app, even by a bug.
-- Deleting the user removes their rows (ON DELETE CASCADE; referential actions bypass RLS).
-- `at` is the database's clock, not a parameter, and the insert policy refuses any other time, so
-- the app can't write a backdated row that the delete policy would then let it remove.

-- +goose Up
CREATE TABLE audit_log (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  at          timestamptz NOT NULL DEFAULT now(),
  action      text NOT NULL CHECK (action ~ '^[a-z][a-z_]*\.[a-z][a-z_]*$' AND char_length(action) <= 48),
  entity_id   text CHECK (char_length(entity_id) <= 64),
  ip          inet,
  browser     text NOT NULL DEFAULT '' CHECK (char_length(browser) <= 64),
  meta        jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(meta) = 'object' AND octet_length(meta::text) <= 4096)
);
CREATE INDEX audit_log_user_id_idx ON audit_log (user_id, id DESC);

REVOKE UPDATE, TRUNCATE ON audit_log FROM finuance_app;

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_log_read ON audit_log FOR SELECT
  USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY audit_log_write ON audit_log FOR INSERT
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid
              AND at BETWEEN now() - interval '1 minute' AND now() + interval '1 minute');
-- no UPDATE policy: with FORCE ROW LEVEL SECURITY nothing may update a row, not even the owner
CREATE POLICY audit_log_expire ON audit_log FOR DELETE
  USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid AND at < now() - interval '365 days');

-- +goose Down
DROP TABLE audit_log;
