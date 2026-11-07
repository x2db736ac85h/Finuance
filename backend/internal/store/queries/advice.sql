-- The advice library (ADR 0034; migration 00021). An instance table: administrators write it,
-- every signed-in user reads the published articles.

-- name: ListAdviceArticles :many
SELECT id, slug, language, title, tags, status, version, created_at, updated_at, char_length(body)::integer AS body_length
FROM advice_articles ORDER BY language, title, id;

-- name: GetAdviceArticle :one
SELECT id, slug, language, title, body, tags, status, version, created_at, updated_at FROM advice_articles WHERE id = @id;

-- name: PublishedAdviceArticle :one
-- The asked language first, then English, then any.
SELECT id, slug, language, title, body, tags, status, version, created_at, updated_at FROM advice_articles
WHERE slug = @slug AND status = 'published'
ORDER BY language = @language DESC, language = 'en' DESC, language LIMIT 1;

-- name: InsertAdviceArticle :one
INSERT INTO advice_articles (id, slug, language, title, body, tags, status, title_n, body_n, created_at, updated_at)
VALUES (@id, @slug, @language, @title, @body, @tags, @status, @title_n, @body_n, @now, @now)
RETURNING id, slug, language, title, body, tags, status, version, created_at, updated_at;

-- name: UpdateAdviceArticle :one
-- Versioned: no row when the version read is no longer the stored one.
UPDATE advice_articles
SET slug = @slug, language = @language, title = @title, body = @body, tags = @tags, status = @status,
    title_n = @title_n, body_n = @body_n, version = version + 1, updated_at = @now
WHERE id = @id AND version = @version
RETURNING id, slug, language, title, body, tags, status, version, created_at, updated_at;

-- name: DeleteAdviceArticle :execrows
DELETE FROM advice_articles WHERE id = @id;

-- name: AdviceSlugs :many
SELECT language, slug FROM advice_articles;

-- name: SearchAdvice :many
-- Published articles matching a tsquery the app built from normalised words (an empty language: any).
SELECT id, slug, language, title, body, ts_rank_cd(search, to_tsquery('simple', @query))::real AS rank
FROM advice_articles
WHERE status = 'published' AND search @@ to_tsquery('simple', @query)
  AND (@language::text = '' OR language = @language)
ORDER BY rank DESC, updated_at DESC, id
LIMIT @max_results;
