# Finuance

**Self-hosted, privacy-first personal finance for European bank CSV exports.**
Import statements from Polish banks (Pekao, PKO BP, mBank, …). Finuance de-duplicates overlapping exports and normalises transactions across accounts. It gives you a fast, keyboard-friendly transaction explorer, interactive insights, a month-centric overview with budgets and recurring payments, and an optional AI assistant that runs on your own LLM, so your data never leaves your server.

> 🚧 **Under construction.** This is a rebuild of the single-file MVP in [`legacy/`](legacy/) as a Go + PostgreSQL + React application. See [`docs/PLAN.md`](docs/PLAN.md) for the plan and [`docs/PROGRESS.md`](docs/PROGRESS.md) for the current status.

## Stack

Go (stdlib `net/http`, spec-first OpenAPI) · PostgreSQL (pgx, sqlc, goose, River, row-level security) · React + TypeScript + Vite · Docker Compose, Helm, Argo CD.

## Getting started (development)

Requirements: Docker (with Compose), Go 1.27, Node 22 (for the legacy oracle and, from Phase 8, the web app).

```sh
make up                       # db → migrate → api (creates .env from .env.example on first run)
curl -s localhost:8080/api/v1/meta
curl -s localhost:8080/readyz
curl -s localhost:9090/metrics | head   # admin port: metrics + pprof (dev only)
make logs                     # follow the logs
make down                     # stop (the database volume is kept; make db-reset wipes it)
```

Day to day:

```sh
make check              # lint, unit tests, codegen drift, fixture drift, private-data guard
make test-integration   # all Go tests, incl. Postgres via testcontainers
make gen                # regenerate code after editing api/openapi.yaml or store/queries/*.sql
make db-up migrate-up dev-api   # run the API on the host against the Compose database
```

## License

[MIT](LICENSE)
