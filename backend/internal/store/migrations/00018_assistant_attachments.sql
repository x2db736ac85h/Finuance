-- Phase 20 (PR 2): photos attached to an assistant question (receipts). The browser re-encodes a
-- photo to a JPEG of at most 1,600 px (EXIF and location dropped); the server checks its size and
-- dimensions before decoding and re-encodes it again. An attachment belongs to its chat (deleted
-- with it, so the chat retention covers photos too); message_seq is the question it went with
-- (NULL until sent; unsent ones are deleted after a day). User-owned, forced RLS.

-- +goose Up
CREATE TABLE assistant_attachments (
  id           uuid PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  chat_id      uuid NOT NULL REFERENCES assistant_chats(id) ON DELETE CASCADE,
  message_seq  integer,
  mime         text NOT NULL CHECK (mime = 'image/jpeg'),
  width        integer NOT NULL CHECK (width BETWEEN 1 AND 2048),
  height       integer NOT NULL CHECK (height BETWEEN 1 AND 2048),
  bytes        bytea NOT NULL CHECK (octet_length(bytes) BETWEEN 1 AND 2097152),
  created_at   timestamptz NOT NULL
);
CREATE INDEX assistant_attachments_chat_idx ON assistant_attachments (chat_id, message_seq);
CREATE INDEX assistant_attachments_user_idx ON assistant_attachments (user_id, created_at);
ALTER TABLE assistant_attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_attachments FORCE ROW LEVEL SECURITY;
CREATE POLICY assistant_attachments_tenant ON assistant_attachments
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE assistant_attachments;
