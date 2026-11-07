-- Phase 20 (PR 3): the advice library (ADR 0034). Articles an administrator writes and publishes;
-- the assistant searches the published ones (search_advice). An instance table, outside RLS like
-- themes. title_n (title and tags) and body_n are the app's normalised text (lower case, accents
-- folded); the generated tsvector uses the 'simple' configuration, so any language works without
-- an extension.

-- +goose Up
CREATE TABLE advice_articles (
  id          uuid PRIMARY KEY,
  slug        text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,63}$'),
  language    text NOT NULL CHECK (language ~ '^[a-z]{2}$'),
  title       text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
  body        text NOT NULL CHECK (char_length(body) <= 20000),
  tags        text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tags) <= 10),
  status      text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
  title_n     text NOT NULL,
  body_n      text NOT NULL,
  search      tsvector GENERATED ALWAYS AS (setweight(to_tsvector('simple', title_n), 'A') ||
                                            setweight(to_tsvector('simple', body_n), 'B')) STORED,
  version     integer NOT NULL DEFAULT 1,
  created_at  timestamptz NOT NULL,
  updated_at  timestamptz NOT NULL,
  UNIQUE (language, slug)
);
CREATE INDEX advice_articles_search_idx ON advice_articles USING gin (search) WHERE status = 'published';

-- +goose Down
DROP TABLE advice_articles;
