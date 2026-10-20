# Backend conventions (Go)

## Package rules
- `ingest`, `money`, `query/dsl` are pure: no DB, no HTTP, no time.Now (use platform.Clock).
- Domain packages (`ledger`, `query`, `insights`, `rules`, `auth`) return domain types, never sqlc rows.
- `httpapi` maps HTTP ⇄ domain only. No SQL strings outside `store` and `query/sql.go`.

## Recipes
- **New endpoint:** edit `api/openapi.yaml` → `make gen` → implement the strict handler method in
  `internal/httpapi/handlers_<area>.go` → map errors in `problems.go` → HTTP test with real DB + tenancy test.
- **New static query:** add to `internal/store/queries/<area>.sql` → `make gen` → wrap in a domain repo → integration test.
- **Dynamic query:** extend `query.ColumnRegistry` / the CTE builder; never concatenate user input; add a fuzz seed.
- **New migration:** `make migrate-new name=…` → SQL with `-- +goose Up/Down` → `make test-integration` (runs up/down/up).
  New user-owned table ⇒ `user_id` column + RLS policy + RLS test.
- **New job:** args struct + worker in `internal/jobs`, register in the registry, enqueue with `InsertTx` inside the
  same transaction as the state change, set `MaxAttempts`, add metrics + a test using River's test helpers.

## Style
- Errors: wrap with `%w`; sentinel errors per package; no panics in request paths.
- `context.Context` first parameter; respect cancellation in loops over rows.
- Table-driven tests; `go-cmp` for diffs; golden files via `-update`.
- Logging: `slog` with request-scoped logger from context; never log bodies, cookies or transaction fields.
