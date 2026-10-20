-- Row-level security (PLAN §5.3, ADR 0007) on every user-owned table. store.WithUser sets
-- app.user_id for one transaction; a row is visible and writable only when its user_id matches.
-- Without the setting, current_setting(…, true) is NULL and every table looks empty.
--
-- FORCE also binds the table owner (finuance_owner), so a migration or an owner connection can't
-- read user data by accident. Referential actions (ON DELETE CASCADE / SET NULL) and constraint
-- checks are not affected: PostgreSQL runs them without row security. A future data migration
-- that must touch every user's rows runs ALTER TABLE … NO FORCE ROW LEVEL SECURITY inside its own
-- transaction and restores FORCE before committing.
--
-- users and sessions are not under RLS: sessions are resolved before a user is known, and only the
-- auth package touches either table.

-- +goose Up

ALTER TABLE user_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY user_preferences_tenant ON user_preferences
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY accounts_tenant ON accounts
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE categories FORCE ROW LEVEL SECURITY;
CREATE POLICY categories_tenant ON categories
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE groups FORCE ROW LEVEL SECURITY;
CREATE POLICY groups_tenant ON groups
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE imports ENABLE ROW LEVEL SECURITY;
ALTER TABLE imports FORCE ROW LEVEL SECURITY;
CREATE POLICY imports_tenant ON imports
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE import_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE import_files FORCE ROW LEVEL SECURITY;
CREATE POLICY import_files_tenant ON import_files
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE import_rows ENABLE ROW LEVEL SECURITY;
ALTER TABLE import_rows FORCE ROW LEVEL SECURITY;
CREATE POLICY import_rows_tenant ON import_rows
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE transactions FORCE ROW LEVEL SECURITY;
CREATE POLICY transactions_tenant ON transactions
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE import_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE import_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY import_profiles_tenant ON import_profiles
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE budgets ENABLE ROW LEVEL SECURITY;
ALTER TABLE budgets FORCE ROW LEVEL SECURITY;
CREATE POLICY budgets_tenant ON budgets
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE tx_annotations ENABLE ROW LEVEL SECURITY;
ALTER TABLE tx_annotations FORCE ROW LEVEL SECURITY;
CREATE POLICY tx_annotations_tenant ON tx_annotations
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE dedupe_overrides ENABLE ROW LEVEL SECURITY;
ALTER TABLE dedupe_overrides FORCE ROW LEVEL SECURITY;
CREATE POLICY dedupe_overrides_tenant ON dedupe_overrides
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE investment_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE investment_profile FORCE ROW LEVEL SECURITY;
CREATE POLICY investment_profile_tenant ON investment_profile
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE investment_positions ENABLE ROW LEVEL SECURITY;
ALTER TABLE investment_positions FORCE ROW LEVEL SECURITY;
CREATE POLICY investment_positions_tenant ON investment_positions
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down

DROP POLICY user_preferences_tenant ON user_preferences;
ALTER TABLE user_preferences NO FORCE ROW LEVEL SECURITY;
ALTER TABLE user_preferences DISABLE ROW LEVEL SECURITY;

DROP POLICY accounts_tenant ON accounts;
ALTER TABLE accounts NO FORCE ROW LEVEL SECURITY;
ALTER TABLE accounts DISABLE ROW LEVEL SECURITY;

DROP POLICY categories_tenant ON categories;
ALTER TABLE categories NO FORCE ROW LEVEL SECURITY;
ALTER TABLE categories DISABLE ROW LEVEL SECURITY;

DROP POLICY groups_tenant ON groups;
ALTER TABLE groups NO FORCE ROW LEVEL SECURITY;
ALTER TABLE groups DISABLE ROW LEVEL SECURITY;

DROP POLICY imports_tenant ON imports;
ALTER TABLE imports NO FORCE ROW LEVEL SECURITY;
ALTER TABLE imports DISABLE ROW LEVEL SECURITY;

DROP POLICY import_files_tenant ON import_files;
ALTER TABLE import_files NO FORCE ROW LEVEL SECURITY;
ALTER TABLE import_files DISABLE ROW LEVEL SECURITY;

DROP POLICY import_rows_tenant ON import_rows;
ALTER TABLE import_rows NO FORCE ROW LEVEL SECURITY;
ALTER TABLE import_rows DISABLE ROW LEVEL SECURITY;

DROP POLICY transactions_tenant ON transactions;
ALTER TABLE transactions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE transactions DISABLE ROW LEVEL SECURITY;

DROP POLICY import_profiles_tenant ON import_profiles;
ALTER TABLE import_profiles NO FORCE ROW LEVEL SECURITY;
ALTER TABLE import_profiles DISABLE ROW LEVEL SECURITY;

DROP POLICY budgets_tenant ON budgets;
ALTER TABLE budgets NO FORCE ROW LEVEL SECURITY;
ALTER TABLE budgets DISABLE ROW LEVEL SECURITY;

DROP POLICY tx_annotations_tenant ON tx_annotations;
ALTER TABLE tx_annotations NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tx_annotations DISABLE ROW LEVEL SECURITY;

DROP POLICY dedupe_overrides_tenant ON dedupe_overrides;
ALTER TABLE dedupe_overrides NO FORCE ROW LEVEL SECURITY;
ALTER TABLE dedupe_overrides DISABLE ROW LEVEL SECURITY;

DROP POLICY investment_profile_tenant ON investment_profile;
ALTER TABLE investment_profile NO FORCE ROW LEVEL SECURITY;
ALTER TABLE investment_profile DISABLE ROW LEVEL SECURITY;

DROP POLICY investment_positions_tenant ON investment_positions;
ALTER TABLE investment_positions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE investment_positions DISABLE ROW LEVEL SECURITY;
