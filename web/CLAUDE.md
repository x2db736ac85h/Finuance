# Frontend conventions (React + TS)

## Rules
- Visual parity with `legacy/finuance.html`. Use tokens from `src/styles/tokens.css`; never hard-code colours.
- CSS sizes in rem (hairlines and SVG text in px). Charts draw in logical units via `<Svg w h>`.
- Server data via TanStack Query hooks in `src/api/hooks`; UI state in Zustand stores. No fetch calls in components.
- No `any`. No `dangerouslySetInnerHTML`. Format money/dates only via `src/lib/format.ts` and `src/lib/dates.ts`.
- Sentence-case copy; actions keep the same verb through the flow; errors explain how to fix.

## Recipes
- **New API usage:** `make gen` (types from openapi.yaml) → hook in `src/api/hooks/<area>.ts` with a stable query key
  → invalidate in the relevant mutations.
- **New chart:** add a `ChartDef` in `src/charts/registry.ts` with `toTable` (table twin is mandatory) and tests.
- **New persisted preference:** add to the persisted slice in `uiStore`/`viewStore`, include in the prefs schema,
  test hydration.
- **New grid column:** add to the column registry (type, label, default width, visibility) and to the backend
  `ColumnRegistry`; extend export.

## Tests
- `npm run test` (Vitest + MSW), `npm run e2e` (Playwright; needs `make up`).
- Screenshot UI changes in dark and light themes for the PR.
