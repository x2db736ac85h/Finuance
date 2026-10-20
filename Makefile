# Finuance — every dev and CI entry point. `make help` lists the targets.
# Targets whose tooling arrives in a later phase print "not yet implemented (Phase N)" and exit 0.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

ENV_FILE ?= .env
COMPOSE  := docker compose --env-file $(ENV_FILE) -f deploy/compose/compose.yaml
FUZZTIME ?= 30s

# Prints the standard placeholder for a target that a later phase implements.
define todo
	@echo "$@: not yet implemented (Phase $(1))"
endef

##@ General

.PHONY: help
help: ## List targets
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n"} \
	  /^##@/ {printf "\n%s\n", substr($$0, 5)} \
	  /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

##@ Quality

.PHONY: check
check: lint typecheck test gen-check check-private ## Lint, typecheck, unit tests, codegen drift, private-data check
	@echo "check: ok"

.PHONY: check-private
check-private: ## Fail on IBAN-like numbers or files under testdata/private/
	@tools/check-private.sh

.PHONY: lint
lint: ## golangci-lint + ESLint
	$(call todo,2)

.PHONY: fmt
fmt: ## gofmt, goimports, Prettier
	$(call todo,2)

.PHONY: typecheck
typecheck: ## tsc --noEmit
	$(call todo,8)

.PHONY: test
test: ## Go unit tests (-short) + Vitest
	$(call todo,2)

.PHONY: test-integration
test-integration: ## All Go tests incl. testcontainers (Docker required)
	$(call todo,2)

.PHONY: test-e2e
test-e2e: ## Playwright against compose.ci.yaml
	$(call todo,9)

.PHONY: fuzz
fuzz: ## All fuzz targets for FUZZTIME (default 30s each)
	$(call todo,3)

.PHONY: bench
bench: ## Go benchmarks with -benchmem
	$(call todo,3)

##@ Code generation

.PHONY: gen
gen: ## sqlc + oapi-codegen + openapi-typescript
	$(call todo,2)

.PHONY: gen-check
gen-check: ## Regenerate and fail on diff
	$(call todo,2)

.PHONY: oracle
oracle: ## Run tools/legacy-oracle -> testdata/oracle/
	$(call todo,1)

##@ Local database

$(ENV_FILE):
	@cp .env.example $(ENV_FILE)
	@echo "Created $(ENV_FILE) from .env.example (dev-only passwords)."

.PHONY: db-up
db-up: $(ENV_FILE) ## Start local Postgres and wait until it is healthy
	$(COMPOSE) up -d --wait db

.PHONY: db-down
db-down: $(ENV_FILE) ## Stop local Postgres (keeps data)
	$(COMPOSE) stop db

.PHONY: db-reset
db-reset: $(ENV_FILE) ## Delete the local database volume and bootstrap again
	$(COMPOSE) down -v --remove-orphans
	$(COMPOSE) up -d --wait db

# Usage: make db-psql                 interactive shell as finuance_owner
#        make db-psql SQL='\du'       run one command (make can't forward -c flags)
#        make db-psql DB_USER=finuance_app
DB_USER ?= finuance_owner
.PHONY: db-psql
db-psql: $(ENV_FILE) ## psql in the finuance database (SQL='…' to run one command)
	$(COMPOSE) exec $(if $(SQL),-T) db psql -U $(DB_USER) -d finuance $(if $(SQL),-c "$(SQL)")

.PHONY: migrate-up migrate-down migrate-status migrate-new
migrate-up: ## Apply migrations (finuance migrate up)
	$(call todo,2)
migrate-down: ## Roll back one migration
	$(call todo,2)
migrate-status: ## Show migration status
	$(call todo,2)
migrate-new: ## Create a migration: make migrate-new name=…
	$(call todo,2)

##@ Running

.PHONY: up
up: ## Full Compose stack (web on http://localhost:8000)
	$(call todo,2)

.PHONY: down
down: ## Stop the full Compose stack
	$(call todo,2)

.PHONY: logs
logs: ## Follow Compose logs
	$(call todo,2)

.PHONY: dev-api dev-worker dev-web
dev-api: ## Run the API locally with .env
	$(call todo,2)
dev-worker: ## Run the worker locally with .env
	$(call todo,2)
dev-web: ## Run the Vite dev server
	$(call todo,8)

.PHONY: seed-demo
seed-demo: ## Demo data for a user: make seed-demo email=…
	$(call todo,2)

.PHONY: llmmock
llmmock: ## Mock OpenAI-compatible server for local UI work
	$(call todo,13)

.PHONY: eval-assistant
eval-assistant: ## Assistant evals against FINUANCE_AI_BASE_URL (MODEL= optional)
	$(call todo,13)

##@ Build and deploy

.PHONY: images
images: ## Build both images locally
	$(call todo,2)

.PHONY: helm-lint
helm-lint: ## helm lint + kubeconform
	$(call todo,16)

.PHONY: loadtest
loadtest: ## k6 against LOADTEST_BASE_URL
	$(call todo,17)
