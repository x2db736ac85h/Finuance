# 0002. Go stdlib `net/http` + spec-first OpenAPI 3.0.3 with `oapi-codegen` strict server

- Status: accepted
- Date: 2026-10-20

## Context

The API serves one SPA we own, plus curl and scripts. We want a typed contract shared by the Go server and the TypeScript client, and compile errors when the two drift apart. Since Go 1.22, `http.ServeMux` supports method and path-parameter patterns, which removes the main reason for a router framework.

## Decision

- **Spec-first:** `api/openapi.yaml` (OpenAPI **3.0.3**) is the single source of truth. Every endpoint change starts there.
- **Server:** `oapi-codegen` v2 with `std-http-server` + `strict-server` + models, generated into `backend/internal/httpapi/gen/` (never edited by hand). Handlers implement the generated `StrictServerInterface` and return typed response objects.
- **Router:** stdlib `net/http` `ServeMux`. Middleware is plain `func(http.Handler) http.Handler`.
- **Validation:** the kin-openapi request validator middleware rejects malformed input at the edge. Domain validation runs in services.
- **Client:** `openapi-typescript` generates `web/src/api/schema.d.ts`; `openapi-fetch` is the runtime client.
- **Errors:** RFC 9457 `application/problem+json`, mapped in one place (`problems.go`).
- `make gen` regenerates everything; `make gen-check` fails CI on drift.

## Consequences (positive, negative, follow-ups)

- Changing a response shape breaks the Go build and the TS typecheck, so contract drift can't reach `main`.
- No framework lock-in; the stdlib is well documented and stable.
- Some features are a little more verbose than in a framework: route groups, and middleware ordering written out by hand.
- 3.0.3 lacks some 3.1 features (`type: [string, "null"]`, full JSON Schema 2020-12). We use `nullable: true` instead. We'll revisit when `oapi-codegen` fully supports 3.1.
- Streaming endpoints (SSE for the assistant, Phase 13) need a hand-written handler next to the strict server. The spec documents them as `text/event-stream`.

## Alternatives considered

- **Code-first** (generate the spec from Go annotations): the spec becomes a by-product and drifts in subtle ways; the TS client would depend on Go build output.
- **chi / echo / gin:** ergonomic, but add a dependency the stdlib now mostly covers.
- **gRPC / Connect:** strong typing, but a browser-first JSON API is simpler to debug, proxy and demo.
- **ogen:** generates fast code, but has a smaller community and less familiar output than `oapi-codegen`.
