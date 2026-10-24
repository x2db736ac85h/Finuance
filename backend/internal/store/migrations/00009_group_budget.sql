-- Phase 15: an optional budget per group (PLAN task 15.9), e.g. a trip budget: the money out the
-- group should stay under, in minor units of the group's currency. NULL: no budget.

-- +goose Up
ALTER TABLE groups ADD COLUMN budget_minor bigint CHECK (budget_minor > 0);

-- +goose Down
ALTER TABLE groups DROP COLUMN budget_minor;
