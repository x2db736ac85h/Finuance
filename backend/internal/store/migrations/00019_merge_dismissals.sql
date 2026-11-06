-- Phase 20 (PR 2, ADR 0032): a card payment typed into a bank account meets the bank's own row
-- when the file is imported; a merge suggestion offers to keep the bank's row with the typed row's
-- category, note, group and shared copy. "Keep both" is remembered here so the pair isn't
-- suggested again. User-owned, forced RLS; deleted with either transaction.

-- +goose Up
CREATE TABLE merge_dismissals (
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  typed_id     uuid NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
  imported_id  uuid NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
  created_at   timestamptz NOT NULL,
  PRIMARY KEY (typed_id, imported_id)
);
CREATE INDEX merge_dismissals_user_idx ON merge_dismissals (user_id);
CREATE INDEX merge_dismissals_imported_idx ON merge_dismissals (imported_id);
ALTER TABLE merge_dismissals ENABLE ROW LEVEL SECURITY;
ALTER TABLE merge_dismissals FORCE ROW LEVEL SECURITY;
CREATE POLICY merge_dismissals_tenant ON merge_dismissals
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE merge_dismissals;
