-- Phase 13: the assistant (PLAN §4.8, §5.2; ADR 0015).
--
-- assistant_settings: what the server enforces per user. retention_days is how long a chat is
-- kept after its last message (1–7 days; NULL = until the user deletes it). A user without a row
-- has the default of 2 days. cloud_consent lists the cloud connections the user agreed to send
-- data to.
--
-- assistant_chats / assistant_messages: one row per model message (user, assistant, tool), so the
-- history sent back to the model is the rows in order. The last assistant row of a turn carries
-- the turn's steps and meta (and error, when the run failed); a failed turn is not sent again.
-- replay holds provider data that must be sent back unchanged (Anthropic thinking blocks).

-- +goose Up
CREATE TABLE assistant_settings (
  user_id         uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  retention_days  smallint DEFAULT 2 CHECK (retention_days BETWEEN 1 AND 7),
  cloud_consent   text[] NOT NULL DEFAULT '{}',
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE assistant_chats (
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       text NOT NULL DEFAULT '' CHECK (char_length(title) <= 200),
  connection  text NOT NULL DEFAULT '',
  model       text NOT NULL DEFAULT '',
  no_tools    boolean NOT NULL DEFAULT false, -- auto mode fell back to plan mode for this chat
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assistant_chats_user_idx ON assistant_chats (user_id, updated_at DESC);

CREATE TABLE assistant_messages (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  chat_id       uuid NOT NULL REFERENCES assistant_chats(id) ON DELETE CASCADE,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  seq           integer NOT NULL,
  role          text NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
  content       text NOT NULL DEFAULT '',
  reasoning     text NOT NULL DEFAULT '',
  tool_calls    jsonb,
  tool_call_id  text NOT NULL DEFAULT '',
  replay        jsonb,
  steps         jsonb,
  meta          jsonb,
  error         jsonb,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (chat_id, seq)
);
CREATE INDEX assistant_messages_user_idx ON assistant_messages (user_id);

ALTER TABLE assistant_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY assistant_settings_tenant ON assistant_settings
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE assistant_chats ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_chats FORCE ROW LEVEL SECURITY;
CREATE POLICY assistant_chats_tenant ON assistant_chats
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE assistant_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_messages FORCE ROW LEVEL SECURITY;
CREATE POLICY assistant_messages_tenant ON assistant_messages
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE assistant_messages;
DROP TABLE assistant_chats;
DROP TABLE assistant_settings;
