# Finuance

**Self-hosted, privacy-first personal finance for European bank CSV exports.**
Import statements from Polish banks (Pekao, PKO BP, mBank, …). Finuance de-duplicates overlapping exports and normalises transactions across accounts. It gives you a fast, keyboard-friendly transaction explorer, interactive insights, a month-centric overview with budgets and recurring payments, and an optional AI assistant that runs on your own LLM, so your data never leaves your server.

> 🚧 **Under construction.** This is a rebuild of the single-file MVP in [`legacy/`](legacy/) as a Go + PostgreSQL + React application. See [`docs/PLAN.md`](docs/PLAN.md) for the plan and [`docs/PROGRESS.md`](docs/PROGRESS.md) for the current status.

## Stack

Go (stdlib `net/http`, spec-first OpenAPI) · PostgreSQL (pgx, sqlc, goose, River, row-level security) · React + TypeScript + Vite · Docker Compose, Helm, Argo CD.

## Getting started (development)

```sh
cp .env.example .env
make db-up      # local PostgreSQL 17 in Docker
make db-psql    # psql shell as the app role
make check      # lint, tests, private-data guard
```

## License

[MIT](LICENSE)
