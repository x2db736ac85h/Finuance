-- Phase 22 (22.12, API-4): every amount a user enters or imports is at most 10^12 minor units in
-- either direction (money.MaxAbsMinor; the API's schema has the same bound), so the totals of the
-- shared and personal views stay far from bigint's limit. NOT VALID: the checks hold for every row
-- written from now on without reading the existing ones, so this migration cannot fail on old data.
-- RUNBOOK "Validating the amount checks" says how to validate them afterwards. balance_after_minor is a value from the
-- bank's file and is never summed, so it has no bound.

-- +goose Up
ALTER TABLE accounts ADD CONSTRAINT accounts_opening_balance_minor_range
  CHECK (opening_balance_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE transactions ADD CONSTRAINT transactions_amount_minor_range
  CHECK (amount_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE budgets ADD CONSTRAINT budgets_amount_minor_range
  CHECK (amount_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE goals ADD CONSTRAINT goals_target_minor_range
  CHECK (target_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE groups ADD CONSTRAINT groups_budget_minor_range
  CHECK (budget_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE shared_groups ADD CONSTRAINT shared_groups_budget_minor_range
  CHECK (budget_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE shared_expenses ADD CONSTRAINT shared_expenses_amount_minor_range
  CHECK (amount_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE shared_expense_splits ADD CONSTRAINT shared_expense_splits_share_minor_range
  CHECK (share_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE shared_settlements ADD CONSTRAINT shared_settlements_amount_minor_range
  CHECK (amount_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE investment_profile ADD CONSTRAINT investment_profile_ppk_employee_minor_range
  CHECK (ppk_employee_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE investment_profile ADD CONSTRAINT investment_profile_ppk_employer_minor_range
  CHECK (ppk_employer_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE investment_profile ADD CONSTRAINT investment_profile_ppk_state_minor_range
  CHECK (ppk_state_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE investment_profile ADD CONSTRAINT investment_profile_ppk_value_minor_range
  CHECK (ppk_value_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE investment_positions ADD CONSTRAINT investment_positions_invested_minor_range
  CHECK (invested_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE investment_positions ADD CONSTRAINT investment_positions_value_minor_range
  CHECK (value_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;
ALTER TABLE investment_positions ADD CONSTRAINT investment_positions_annual_income_minor_range
  CHECK (annual_income_minor BETWEEN -1000000000000 AND 1000000000000) NOT VALID;

-- +goose Down
ALTER TABLE accounts DROP CONSTRAINT accounts_opening_balance_minor_range;
ALTER TABLE transactions DROP CONSTRAINT transactions_amount_minor_range;
ALTER TABLE budgets DROP CONSTRAINT budgets_amount_minor_range;
ALTER TABLE goals DROP CONSTRAINT goals_target_minor_range;
ALTER TABLE groups DROP CONSTRAINT groups_budget_minor_range;
ALTER TABLE shared_groups DROP CONSTRAINT shared_groups_budget_minor_range;
ALTER TABLE shared_expenses DROP CONSTRAINT shared_expenses_amount_minor_range;
ALTER TABLE shared_expense_splits DROP CONSTRAINT shared_expense_splits_share_minor_range;
ALTER TABLE shared_settlements DROP CONSTRAINT shared_settlements_amount_minor_range;
ALTER TABLE investment_profile DROP CONSTRAINT investment_profile_ppk_employee_minor_range;
ALTER TABLE investment_profile DROP CONSTRAINT investment_profile_ppk_employer_minor_range;
ALTER TABLE investment_profile DROP CONSTRAINT investment_profile_ppk_state_minor_range;
ALTER TABLE investment_profile DROP CONSTRAINT investment_profile_ppk_value_minor_range;
ALTER TABLE investment_positions DROP CONSTRAINT investment_positions_invested_minor_range;
ALTER TABLE investment_positions DROP CONSTRAINT investment_positions_value_minor_range;
ALTER TABLE investment_positions DROP CONSTRAINT investment_positions_annual_income_minor_range;
