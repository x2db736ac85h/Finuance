-- Baseline: intentionally empty. It exercises the migration pipeline (goose, then River) before the
-- core schema arrives in Phase 4 (00002_core.sql). Roles, the database and extensions are created by
-- deploy/db/bootstrap.sql, which needs superuser rights and is therefore not a migration.

-- +goose Up
SELECT 1;

-- +goose Down
SELECT 1;
