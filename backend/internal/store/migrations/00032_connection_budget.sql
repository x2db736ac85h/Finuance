-- Phase 26 (26.4, ADR 0045): a budget per connection. A cloud model's cost falls on whoever runs
-- the instance, so an administrator can give a connection a number of tokens per UTC day, counted
-- across all users.
--   assistant_connection_controls.daily_tokens   the budget; NULL for none
--   assistant_connection_usage                   what each connection used on each day: the tokens
--                                                of finished runs (reported by the provider where
--                                                it reports them) and how many runs
-- An instance table like the controls: counts per connection, nothing of any user.

-- +goose Up
ALTER TABLE assistant_connection_controls
  ADD COLUMN daily_tokens bigint CHECK (daily_tokens BETWEEN 1000 AND 1000000000000);

CREATE TABLE assistant_connection_usage (
  connection  text NOT NULL CHECK (connection ~ '^[a-z0-9][a-z0-9_-]{0,39}$'),
  day         date NOT NULL,
  tokens      bigint NOT NULL DEFAULT 0 CHECK (tokens >= 0),
  runs        integer NOT NULL DEFAULT 0 CHECK (runs >= 0),
  PRIMARY KEY (connection, day)
);

-- +goose Down
DROP TABLE assistant_connection_usage;
ALTER TABLE assistant_connection_controls DROP COLUMN daily_tokens;
