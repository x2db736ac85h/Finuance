-- Legacy migrations (PLAN §4.10; migration 00007).

-- name: CreateLegacyImport :one
INSERT INTO legacy_imports (id, user_id, manifest, progress, created_at)
VALUES (@id, @user_id, @manifest, @progress, @now)
RETURNING id, user_id, status, progress, report, error, created_at, finished_at;

-- name: AddLegacyImportFile :exec
INSERT INTO legacy_import_files (legacy_import_id, position, user_id, content)
VALUES (@legacy_import_id, @position, @user_id, @content);

-- name: GetLegacyImport :one
-- Without the manifest (the job reads it with GetLegacyImportManifest).
SELECT id, user_id, status, progress, report, error, created_at, finished_at
FROM legacy_imports WHERE user_id = @user_id AND id = @id;

-- name: ListLegacyImports :many
SELECT id, user_id, status, progress, report, error, created_at, finished_at
FROM legacy_imports WHERE user_id = @user_id ORDER BY created_at DESC, id LIMIT 10;

-- name: GetLegacyImportManifest :one
SELECT manifest FROM legacy_imports WHERE user_id = @user_id AND id = @id;

-- name: LegacyImportFile :one
SELECT content FROM legacy_import_files
WHERE user_id = @user_id AND legacy_import_id = @legacy_import_id AND position = @position;

-- name: SetLegacyImportProgress :exec
UPDATE legacy_imports SET status = 'running', progress = @progress
WHERE user_id = @user_id AND id = @id AND status IN ('queued', 'running');

-- name: FinishLegacyImport :exec
-- The end of a run: the report (or the error), and the manifest and files are no longer needed.
UPDATE legacy_imports
SET status = @status, report = sqlc.narg(report), error = sqlc.narg(error), finished_at = @now, manifest = '{}'
WHERE user_id = @user_id AND id = @id;

-- name: DeleteLegacyImportFiles :exec
DELETE FROM legacy_import_files WHERE user_id = @user_id AND legacy_import_id = @legacy_import_id;
