-- Core schema (PLAN §5.2). Every user-owned table carries user_id; row-level security is added by
-- 00003_rls.sql. Ids are UUIDv7 generated in Go (platform.NewID), money is bigint minor units
-- (ADR 0004), booking dates are date (ADR 0005). The *_n columns hold ingest.Collapse(ingest.Norm(x)),
-- computed in Go so search and filters share one normalisation.

-- +goose Up
CREATE COLLATION IF NOT EXISTS pl_natural (provider = icu, locale = 'pl-u-kn-true');

CREATE TABLE users (
  id             uuid PRIMARY KEY,
  email          citext NOT NULL UNIQUE,
  password_hash  text NOT NULL,                       -- PHC string: $argon2id$v=19$m=…,t=…,p=…$salt$hash
  display_name   text NOT NULL DEFAULT '',
  is_demo        boolean NOT NULL DEFAULT false,
  created_at     timestamptz NOT NULL DEFAULT now(),
  last_login_at  timestamptz
);

CREATE TABLE sessions (
  token_hash    bytea PRIMARY KEY,                     -- SHA-256 of the random cookie token; raw token never stored
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at    timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,
  user_agent    text NOT NULL DEFAULT '',
  ip            inet
);
CREATE INDEX sessions_user_idx    ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE user_preferences (
  user_id     uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  prefs       jsonb NOT NULL DEFAULT '{}'::jsonb,       -- theme, uiScale, paneHeight, cards, chart cfg, cardOrder, AI panel, per-view state (§7.4)
  version     integer NOT NULL DEFAULT 1,               -- optimistic concurrency
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE accounts (
  id                     uuid PRIMARY KEY,
  user_id                uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name                   text NOT NULL,
  name_n                 text NOT NULL,
  currency               char(3) NOT NULL DEFAULT 'PLN',
  number_hint            text,                          -- last 4 digits only; never the full account number
  opening_balance_minor  bigint NOT NULL DEFAULT 0,
  is_demo                boolean NOT NULL DEFAULT false,
  archived_at            timestamptz,
  created_at             timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, name_n)
);

CREATE TABLE categories (
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        text NOT NULL,
  name_n      text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, name_n)
);

CREATE TABLE groups (                                   -- user-defined transaction groups (§4.7)
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
  name_n      text NOT NULL,
  color       smallint NOT NULL CHECK (color BETWEEN 0 AND 7),
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, name_n)
);

CREATE TABLE imports (
  id              uuid PRIMARY KEY,
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id      uuid REFERENCES accounts(id) ON DELETE CASCADE,   -- NULL until chosen in preview
  filename        text NOT NULL,
  size_bytes      integer NOT NULL,
  sha256          bytea NOT NULL,
  status          text NOT NULL CHECK (status IN ('previewed','committing','committed','failed')),
  encoding        text NOT NULL,
  delimiter       text NOT NULL,
  headers         jsonb NOT NULL,                       -- ["Data księgowania", …]
  column_types    jsonb NOT NULL,                       -- ["date","date","text",…]
  mapping         jsonb NOT NULL,                       -- §4.3
  options         jsonb NOT NULL,                       -- §4.3
  mapping_source  text NOT NULL CHECK (mapping_source IN ('detected','profile','user')),
  header_signature bytea NOT NULL,                     -- links to import_profiles; drives the sidebar source labels b1, b2, …
  stats           jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {rows, errors, new, duplicates, headerAt, dropped}
  error           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  committed_at    timestamptz
);
CREATE UNIQUE INDEX imports_user_sha_uniq ON imports (user_id, sha256) WHERE status <> 'failed';
CREATE INDEX imports_user_created_idx ON imports (user_id, created_at DESC);
CREATE INDEX imports_account_idx      ON imports (account_id);

CREATE TABLE import_files (                             -- raw bytes kept for lossless re-processing
  import_id  uuid PRIMARY KEY REFERENCES imports(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content    bytea NOT NULL
);

CREATE TABLE transactions (
  id                    uuid PRIMARY KEY,
  seq                   bigint GENERATED ALWAYS AS IDENTITY,
  user_id               uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id            uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  origin                text NOT NULL DEFAULT 'import' CHECK (origin IN ('import','manual')),
  fingerprint           bytea NOT NULL,
  occurrence            integer NOT NULL CHECK (occurrence >= 1),
  split_scope           uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',  -- keep-both overrides (§4.4)
  source_import_id      uuid REFERENCES imports(id) ON DELETE SET NULL,            -- where the field values come from
  booked_on             date NOT NULL,
  value_on              date,
  amount_minor          bigint NOT NULL,
  currency              char(3) NOT NULL,
  payee                 text NOT NULL DEFAULT '',
  payee_n               text NOT NULL DEFAULT '',
  payee_group           text NOT NULL DEFAULT '(unknown)',
  description           text NOT NULL DEFAULT '',
  description_n         text NOT NULL DEFAULT '',
  reference             text NOT NULL DEFAULT '',
  reference_n           text NOT NULL DEFAULT '',
  tx_type               text NOT NULL DEFAULT '',
  tx_type_n             text NOT NULL DEFAULT '',
  bank_category         text NOT NULL DEFAULT '',
  bank_category_n       text NOT NULL DEFAULT '',
  counterparty_account  text NOT NULL DEFAULT '',
  counterparty_n        text NOT NULL DEFAULT '',
  balance_after_minor   bigint,
  category_id           uuid REFERENCES categories(id) ON DELETE SET NULL,
  category_source       text NOT NULL DEFAULT 'none' CHECK (category_source IN ('none','bank','rule','manual')),
  group_id              uuid REFERENCES groups(id) ON DELETE SET NULL,  -- §4.7; mirrored in tx_annotations
  is_transfer           boolean NOT NULL DEFAULT false,               -- Phase 15
  transfer_peer_id      uuid REFERENCES transactions(id) ON DELETE SET NULL,
  note                  text NOT NULL DEFAULT '',
  note_n                text NOT NULL DEFAULT '',
  raw                   jsonb NOT NULL DEFAULT '[]'::jsonb,           -- [{"h":"Kwota operacji","v":"-44,98"}, …]
  search_text           text NOT NULL,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT transactions_dedupe UNIQUE (account_id, fingerprint, occurrence, split_scope)
);
CREATE INDEX tx_user_booked_idx   ON transactions (user_id, booked_on DESC, seq);
CREATE INDEX tx_account_booked_idx ON transactions (account_id, booked_on);
CREATE INDEX tx_user_category_idx ON transactions (user_id, category_id);
CREATE INDEX tx_user_payee_idx    ON transactions (user_id, payee_group);
CREATE INDEX tx_user_group_idx    ON transactions (user_id, group_id);
CREATE INDEX tx_source_import_idx ON transactions (source_import_id);
CREATE INDEX tx_search_trgm_idx   ON transactions USING gin (search_text gin_trgm_ops);

CREATE TABLE import_rows (                              -- staging AND provenance
  import_id       uuid NOT NULL REFERENCES imports(id) ON DELETE CASCADE,
  row_index       integer NOT NULL,                     -- 0-based data row index in the file
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  -- same canonical columns as transactions, all NULLable (errored rows may lack them):
  fingerprint bytea, occurrence integer, split_scope uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000', booked_on date, value_on date, amount_minor bigint, currency char(3),
  payee text, payee_n text, payee_group text, description text, description_n text,
  reference text, reference_n text, tx_type text, tx_type_n text, bank_category text, bank_category_n text,
  counterparty_account text, counterparty_n text, balance_after_minor bigint,
  raw             jsonb NOT NULL,
  search_text     text,
  parse_error     text,
  transaction_id  uuid REFERENCES transactions(id) ON DELETE SET NULL,
  PRIMARY KEY (import_id, row_index)
);
CREATE INDEX import_rows_tx_idx ON import_rows (transaction_id);

CREATE TABLE import_profiles (
  user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  header_signature  bytea NOT NULL,
  name              text NOT NULL DEFAULT '',
  mapping           jsonb NOT NULL,
  options           jsonb NOT NULL,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, header_signature)
);

CREATE TABLE budgets (                                  -- global per category (legacy behaviour)
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category_id   uuid NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  amount_minor  bigint NOT NULL CHECK (amount_minor >= 0),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, category_id)
);


CREATE TABLE tx_annotations (                           -- user edits keyed by dedupe identity (§4.4)
  account_id         uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  fingerprint        bytea NOT NULL,
  occurrence         integer NOT NULL,
  split_scope        uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  user_id            uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  group_id           uuid REFERENCES groups(id) ON DELETE SET NULL,
  category_id        uuid REFERENCES categories(id) ON DELETE SET NULL,   -- manual categories only
  note               text NOT NULL DEFAULT '',
  note_n             text NOT NULL DEFAULT '',
  transfer_override  boolean,                                             -- Phase 15: manual pair/unpair
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, fingerprint, occurrence, split_scope)
);

CREATE TABLE dedupe_overrides (                         -- duplicate review (§4.4, legacy §32.3)
  account_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  fingerprint          bytea NOT NULL,
  user_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  mode                 text NOT NULL CHECK (mode IN ('prefer','keep_both')),
  preferred_import_id  uuid REFERENCES imports(id) ON DELETE CASCADE,   -- deleting it removes the override
  updated_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, fingerprint),
  CHECK ((mode = 'prefer') = (preferred_import_id IS NOT NULL))
);

CREATE TABLE investment_profile (                       -- manual investment data (§4.9, legacy §32.4)
  user_id             uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  ppk_employee_minor  bigint NOT NULL DEFAULT 0 CHECK (ppk_employee_minor >= 0),
  ppk_employer_minor  bigint NOT NULL DEFAULT 0 CHECK (ppk_employer_minor >= 0),
  ppk_state_minor     bigint NOT NULL DEFAULT 0 CHECK (ppk_state_minor >= 0),
  ppk_value_minor     bigint NOT NULL DEFAULT 0 CHECK (ppk_value_minor >= 0),
  swr_bp              integer NOT NULL DEFAULT 400 CHECK (swr_bp BETWEEN 50 AND 1000),
  real_return_bp      integer NOT NULL DEFAULT 500 CHECK (real_return_bp BETWEEN -1000 AND 2000),
  version             integer NOT NULL DEFAULT 1,
  updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE investment_positions (
  id                   uuid PRIMARY KEY,
  user_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  position             integer NOT NULL,
  name                 text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
  type                 text NOT NULL DEFAULT '' CHECK (char_length(type) <= 30),
  invested_minor       bigint NOT NULL DEFAULT 0 CHECK (invested_minor >= 0),
  value_minor          bigint NOT NULL DEFAULT 0 CHECK (value_minor >= 0),
  annual_income_minor  bigint NOT NULL DEFAULT 0 CHECK (annual_income_minor >= 0),
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX investment_positions_user_idx ON investment_positions (user_id, position);

-- +goose Down
DROP TABLE investment_positions;
DROP TABLE investment_profile;
DROP TABLE dedupe_overrides;
DROP TABLE tx_annotations;
DROP TABLE budgets;
DROP TABLE import_profiles;
DROP TABLE import_rows;
DROP TABLE transactions;
DROP TABLE import_files;
DROP TABLE imports;
DROP TABLE groups;
DROP TABLE categories;
DROP TABLE accounts;
DROP TABLE user_preferences;
DROP TABLE sessions;
DROP TABLE users;
DROP COLLATION pl_natural;
