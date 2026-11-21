-- Phase 24 (task 24.7, ADR 0041): who is synthetic is a fact about the user, decided by the server.
-- A synthetic user is one the traffic simulator signs in as (Phase 25). Its requests and jobs are
-- labelled as synthetic in logs, metrics and traces, so they can be shown or left out of the
-- numbers; no request can set or clear the label, because it is read from this column.
--   users.kind   'person' (everyone until now) or 'synthetic'. A synthetic user is never an
--                administrator; the table refuses the combination.
-- users.is_demo stays as it is: a demo visitor is a person trying the product.

-- +goose Up
ALTER TABLE users
  ADD COLUMN kind text NOT NULL DEFAULT 'person',
  ADD CONSTRAINT users_kind_check CHECK (kind IN ('person', 'synthetic')),
  ADD CONSTRAINT users_synthetic_not_admin CHECK (NOT (kind = 'synthetic' AND is_admin));

-- +goose Down
ALTER TABLE users
  DROP CONSTRAINT users_synthetic_not_admin,
  DROP CONSTRAINT users_kind_check,
  DROP COLUMN kind;
