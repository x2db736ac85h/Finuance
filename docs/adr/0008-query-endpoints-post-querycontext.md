# 0008. Query endpoints use `POST` with a `QueryContext`; offset windows for the virtual grid

- Status: accepted
- Date: 2026-10-20

## Context

The table, insights, overview, export and distinct-values endpoints all scope their results by the same state: selected accounts, global search, per-column filters (with value checklists that can hold hundreds of entries), a period and transfer handling. Encoding that in a query string is fragile, length-limited and hard to type. The grid is virtualised (TanStack Virtual) and needs random access to any row range: jumping to the end, or dragging the scrollbar.

## Decision

- Read endpoints that take a scope use **`POST …/query`** with a JSON **`QueryContext`** body: `{accountIds, search, filters, period, includeTransfers}`. They are safe and idempotent by contract, even though the method is POST. Simple lookups by id stay `GET`.
- The server compiles a `QueryContext` once into shared `base`/`scoped` CTEs (`internal/query`), reused by every endpoint, so the table and charts always agree.
- The grid loads rows in fixed-size **offset windows** (`{context, sort, offset, limit}`). The client caches each window under `['tx', ctxKey, sortKey, windowIndex]`, where `ctxKey` is a stable hash of the QueryContext. Each response includes the total count.
- Range selection across windows that haven't loaded uses `POST /transactions/ids` (ids + amounts only).

## Consequences (positive, negative, follow-ups)

- One typed context object for every view; the OpenAPI schema validates it.
- Offset windows give random access and a correct scrollbar. Deep offsets cost more on very large tables, so it's mitigated by indexes, a window size tuned in Phase 7, and `EXPLAIN` reviews.
- POST responses aren't HTTP-cacheable. TanStack Query does the caching client-side.
- Rows can shift between window fetches if data changes. Mutations invalidate the affected windows.

## Alternatives considered

- **GET with query-string encoding:** URL length limits, awkward arrays and nested filters, and poor typing.
- **Keyset (cursor) pagination:** stable and fast, but no random access, which a virtual scrollbar needs.
- **A `QUERY` HTTP method:** semantically right, but not yet supported across proxies, `oapi-codegen` and browsers.
- **GraphQL:** solves query shape, but adds a large stack for a single client.
