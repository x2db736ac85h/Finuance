-- Phase 26 (26.9, ADR 0045): a Google connection may use the anthropic format too (Claude on
-- Vertex AI). Migration 00031's check allowed the openai format only.

-- +goose Up
ALTER TABLE assistant_connections DROP CONSTRAINT assistant_connections_google_check;
ALTER TABLE assistant_connections ADD CONSTRAINT assistant_connections_google_check CHECK (
    (auth = 'google' AND cloud AND api_key IS NULL
       AND project ~ '^[a-z][a-z0-9-]{4,28}[a-z0-9]$' AND location ~ '^[a-z0-9-]{2,40}$')
    OR (auth = 'key' AND project = '' AND location = ''));

-- +goose Down
-- going back, a Claude-on-Vertex connection has no place: it goes
DELETE FROM assistant_connections WHERE auth = 'google' AND api <> 'openai';
ALTER TABLE assistant_connections DROP CONSTRAINT assistant_connections_google_check;
ALTER TABLE assistant_connections ADD CONSTRAINT assistant_connections_google_check CHECK (
    (auth = 'google' AND cloud AND api_key IS NULL AND api = 'openai'
       AND project ~ '^[a-z][a-z0-9-]{4,28}[a-z0-9]$' AND location ~ '^[a-z0-9-]{2,40}$')
    OR (auth = 'key' AND project = '' AND location = ''));
