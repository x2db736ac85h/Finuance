-- Phase 27 (F15, ADR 0046): the assistant's memory. One note per person, in their own words or in
-- lines the assistant proposed, added to the system prompt of their questions.
--   assistant_settings.memory          the note; at most 4,000 characters (about 1,000 tokens)
--   assistant_settings.memory_enabled  whether the note goes into the prompt (on by default; an
--                                      empty note adds nothing either way)
--   assistant_settings.memory_auto     whether a line the assistant proposes to remember is added
--                                      without a click (off by default)
-- The table is the person's own (row-level security since 00006) and goes with the account.

-- +goose Up
ALTER TABLE assistant_settings
  ADD COLUMN memory text NOT NULL DEFAULT '' CHECK (char_length(memory) <= 4000),
  ADD COLUMN memory_enabled boolean NOT NULL DEFAULT true,
  ADD COLUMN memory_auto boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE assistant_settings
  DROP COLUMN memory_auto,
  DROP COLUMN memory_enabled,
  DROP COLUMN memory;
