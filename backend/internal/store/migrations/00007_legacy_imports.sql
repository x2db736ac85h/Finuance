-- Phase 14: migrating a backup of the single-file legacy app (PLAN §4.10, task 14.1).
--
-- legacy_imports: one migration run. manifest is what the browser sent (the decrypted settings,
-- labels, budgets, investments and chats, and per file its legacy settings and target account);
-- it is cleared when the run ends, so chats copied into it don't outlive the user's retention.
-- progress and report are shown by the wizard. At most one run per user is queued or running.
--
-- legacy_import_files: the files' bytes until the run has turned them into imports (deleted at
-- the end of the run; the imports keep their own copy).

-- +goose Up
CREATE TABLE legacy_imports (
  id           uuid PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
  manifest     jsonb NOT NULL DEFAULT '{}',
  progress     jsonb NOT NULL DEFAULT '{}',
  report       jsonb,
  error        text,
  created_at   timestamptz NOT NULL DEFAULT now(),
  finished_at  timestamptz
);
CREATE INDEX legacy_imports_user_idx ON legacy_imports (user_id, created_at DESC);
CREATE UNIQUE INDEX legacy_imports_one_active ON legacy_imports (user_id) WHERE status IN ('queued', 'running');

CREATE TABLE legacy_import_files (
  legacy_import_id  uuid NOT NULL REFERENCES legacy_imports(id) ON DELETE CASCADE,
  position          integer NOT NULL CHECK (position >= 0),
  user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content           bytea NOT NULL,
  PRIMARY KEY (legacy_import_id, position)
);
CREATE INDEX legacy_import_files_user_idx ON legacy_import_files (user_id);

ALTER TABLE legacy_imports ENABLE ROW LEVEL SECURITY;
ALTER TABLE legacy_imports FORCE ROW LEVEL SECURITY;
CREATE POLICY legacy_imports_tenant ON legacy_imports
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE legacy_import_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE legacy_import_files FORCE ROW LEVEL SECURITY;
CREATE POLICY legacy_import_files_tenant ON legacy_import_files
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE legacy_import_files;
DROP TABLE legacy_imports;
