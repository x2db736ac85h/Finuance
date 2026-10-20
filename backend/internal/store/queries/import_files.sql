-- name: InsertImportFile :exec
INSERT INTO import_files (import_id, user_id, content) VALUES (@import_id, @user_id, @content);

-- name: GetImportFile :one
SELECT content FROM import_files WHERE user_id = @user_id AND import_id = @import_id;
