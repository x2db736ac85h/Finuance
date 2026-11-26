# Finuance — every dev and CI entry point. `make help` lists the targets.
# Targets whose tooling arrives in a later phase print "not yet implemented (Phase N)" and exit 0.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

ENV_FILE ?= .env
ORACLE   := tools/legacy-oracle
COMPOSE  := docker compose --env-file $(ENV_FILE) -f deploy/compose/compose.yaml
COMPOSE_CI := $(COMPOSE) -f deploy/compose/compose.ci.yaml
# Load test (PLAN 17.5): k6 pinned like the rehearsal's; the plan's size is 50 users × 20,000 rows.
K6_IMAGE := grafana/k6:2.4.0@sha256:d41bb79cd8de5a7ad619b53570e3712633e8bff0937010a5c6c57a4747e7a276
LOADTEST_BASE_URL ?= http://localhost:8000
LOADTEST_USERS ?= 50
LOADTEST_ROWS ?= 20000
LOADTEST_PASSWORD ?= load test password
# On a lab (FINUANCE_SIMULATION=true make up) the load test's users are the simulator's animals
# (ADR 0044): finuance-sim gives them their history over LOADTEST_YEARS and writes their passwords
# for k6 (loadtest/users.json, from a seed of their own in loadtest/.sim-seed). Their traffic stays
# out of the instance's service-level numbers. LOADTEST_PEOPLE=1 seeds people instead, for an
# instance that is not a lab.
LOADTEST_YEARS ?= 3
LOADTEST_PEOPLE ?=
LOADTEST_SEED := loadtest/.sim-seed
PROFILE ?= load
FUZZTIME ?= 30s
BIN      := backend/bin
SQLC     := $(BIN)/sqlc
GOLANGCI := $(BIN)/golangci-lint
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
UP_FLAGS ?= --build
# Loads .env into the environment of host processes (dev-api, migrate-*).
RUN_ENV  := set -a; . ./$(ENV_FILE); set +a;

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
check: lint typecheck test gen-check fixtures-check check-private check-env-details check-sealed check-toolchain check-no-float check-compose-prod check-db-bootstrap check-port-forwards check-pins check-workflows check-zap-hooks ## Lint, typecheck, unit tests, codegen drift, fixture drift, private-data, environment-details, sealed-secret, toolchain, float, prod-Compose, k8s bootstrap, port-forward, pin, workflow and ZAP hook checks
	@echo "check: ok"

.PHONY: check-compose-prod
check-compose-prod: ## compose.prod.yaml's inline database bootstrap equals deploy/db/
	@tools/check-compose-prod.sh

.PHONY: check-db-bootstrap
check-db-bootstrap: ## The CloudNativePG bootstrap SQL (deploy/k8s/db) matches deploy/db/bootstrap.sql
	@tools/check-db-bootstrap.sh

.PHONY: check-pins
check-pins: ## Every action by commit SHA and every image the repository doesn't build by digest
	@tools/check-pins.sh
	@tools/check-pins-test.sh

.PHONY: check-zap-hooks
check-zap-hooks: ## The ZAP baseline's hook drops the report-uri notice and keeps every other CSP finding
	@out=$$(python3 -I -B .zap/baseline_hooks_test.py 2>&1) || { echo "$$out"; exit 1; }; echo "check-zap-hooks: ok"

.PHONY: check-workflows
check-workflows: ## actionlint and zizmor (offline) over the workflows, when tools/bin-tools.sh installed them; CI always runs them, actionlint with shellcheck (have shellcheck on PATH to match)
	@if [ -x backend/bin/actionlint ] && [ -x backend/bin/zizmor ]; then \
	  sc=""; if shellcheck --version >/dev/null 2>&1; then sc=shellcheck; fi; \
	  backend/bin/actionlint -shellcheck="$$sc" && backend/bin/zizmor --offline --quiet .github/workflows && echo "check-workflows: ok"; \
	else echo "check-workflows: skipped (tools/bin-tools.sh actionlint and zizmor install them)"; fi

.PHONY: check-toolchain
check-toolchain: ## Fail when go.mod's toolchain, mise.toml and Dockerfile.backend name different Go versions
	@tools/check-toolchain.sh

.PHONY: check-port-forwards
check-port-forwards: ## The admin tools' port-forwards use one local port everywhere, none of the Compose stack's; make observe's self-test
	@tools/check-port-forwards.sh
	@tools/observe-test.sh

.PHONY: observe
observe: ## Port-forwards to Grafana, Prometheus, Alertmanager and Argo CD on this machine's loopback (docs/RUNBOOK.md "Local ports"); TOOLS="prometheus grafana" for some
	@tools/observe.sh $(TOOLS)

.PHONY: check-sealed
check-sealed: ## Every *.sealed.yaml is a SealedSecret without plain values (gitleaks allowlists these files)
	@tools/check-sealed.sh

.PHONY: check-env-details
check-env-details: ## Fail on private environment details (patterns from the private environment repository; skipped without it)
	@tools/check-env-details.sh

.PHONY: check-private
check-private: ## Fail on account numbers, e-mail addresses, tailnet names, postcodes in test data, or files under testdata/private/
	@tools/check-private.sh
	@tools/check-private-test.sh
	@tools/publish-public-test.sh

.PHONY: check-no-float
check-no-float: ## Fail on floating point in internal/money or the fingerprint path
	@tools/check-no-float.sh

COVER_PKGS := ./internal/ingest/ ./internal/money/
COVER_MIN  ?= 90
.PHONY: coverage
coverage: ## Line coverage of ingest + money; fails below COVER_MIN (default 90 %)
	@cd backend && go test -short -coverprofile=cover.out $(COVER_PKGS) >/dev/null && \
	  total="$$(go tool cover -func=cover.out | awk '/^total:/ {sub("%", "", $$3); print $$3}')" && rm -f cover.out && \
	  echo "coverage: ingest + money $$total % (minimum $(COVER_MIN) %)" && \
	  awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { exit !(t >= m) }'

.PHONY: lint
lint: $(GOLANGCI) web/node_modules ## golangci-lint + ESLint + Prettier check
	cd backend && bin/golangci-lint run ./...
	cd web && npm run --silent lint

.PHONY: fmt
fmt: $(GOLANGCI) web/node_modules ## gofmt + goimports + Prettier
	cd backend && bin/golangci-lint fmt ./...
	cd web && npm run --silent format

.PHONY: typecheck
typecheck: web/node_modules ## tsc --noEmit
	cd web && npm run --silent typecheck

.PHONY: test
test: web/node_modules ## Go unit tests (-short) + Vitest
	cd backend && go vet ./... && go test -short ./...
	cd web && npm run --silent test && node scripts/check-native.mjs --self-test && node scripts/check-native.mjs

.PHONY: test-integration
test-integration: ## All Go tests incl. testcontainers (Docker required)
	cd backend && go test ./...

.PHONY: test-e2e
# The E2E administrator (admin.spec.ts, Phase 19): created with the CLI, as on a real instance.
E2E_ADMIN_EMAIL ?= admin@example.test
E2E_ADMIN_PASSWORD ?= e2e admin password long enough

test-e2e: $(ENV_FILE) web-dist ## Playwright against a fresh compose.ci.yaml stack on :8000 (KEEP=1 leaves it running)
	VERSION=$(VERSION) $(COMPOSE_CI) up -d --build --wait
	printf '%s\n' '$(E2E_ADMIN_PASSWORD)' | $(COMPOSE_CI) exec -T api /finuance admin create-user -admin \
	  -email=$(E2E_ADMIN_EMAIL) -display-name=Admin -password-stdin || true # already there with KEEP=1
	status=0; (cd web && E2E_BASE_URL=http://localhost:8000 npx playwright test) || status=$$?; \
	  $(if $(KEEP),,$(COMPOSE_CI) down -v;) exit $$status

.PHONY: check-access-log
check-access-log: $(ENV_FILE) ## On the running compose.ci.yaml stack (test-e2e KEEP=1): no user text in the web and api logs, one request id over both hops
	@COMPOSE="$(COMPOSE_CI)" tools/check-access-log.sh

.PHONY: test-e2e-demo
test-e2e-demo: $(ENV_FILE) web-dist ## Playwright's demo-mode specs against compose.ci.yaml + compose.demo.yaml (KEEP=1 leaves it running)
	VERSION=$(VERSION) $(COMPOSE_CI) -f deploy/compose/compose.demo.yaml up -d --build --wait
	status=0; (cd web && E2E_DEMO=1 E2E_BASE_URL=http://localhost:8000 npx playwright test) || status=$$?; \
	  $(if $(KEEP),,$(COMPOSE_CI) -f deploy/compose/compose.demo.yaml down -v;) exit $$status

.PHONY: fuzz
fuzz: ## All pure fuzz targets for FUZZTIME (default 30s each); *DB targets run in the nightly workflow
	@cd backend && for pkg in $$(go list ./...); do \
	  for f in $$(go test -list '^Fuzz' "$$pkg" | grep '^Fuzz' | grep -v 'DB$$' || true); do \
	    echo "fuzz: $$f ($$pkg) for $(FUZZTIME)"; \
	    go test -run '^$$' -fuzz "^$$f$$" -fuzztime $(FUZZTIME) "$$pkg"; \
	  done; done

.PHONY: fuzz-db
fuzz-db: ## Fuzz targets that need Postgres (Docker), FUZZTIME each (nightly CI: 60s)
	@cd backend && for pkg in $$(go list ./...); do \
	  for f in $$(go test -short -list '^Fuzz.*DB$$' "$$pkg" | grep '^Fuzz' || true); do \
	    echo "fuzz: $$f ($$pkg) for $(FUZZTIME)"; \
	    go test -run '^$$' -fuzz "^$$f$$" -fuzztime $(FUZZTIME) -parallel 1 "$$pkg"; \
	  done; done

.PHONY: bench
bench: ## Go benchmarks with -benchmem
	cd backend && go test -short -run='^$$' -bench=. -benchmem ./...

##@ Code generation

$(SQLC) $(GOLANGCI): tools/bin-tools.sh
	tools/bin-tools.sh $(notdir $@)

.PHONY: gen
gen: $(SQLC) web/node_modules ## oapi-codegen + sqlc + openapi-typescript
	cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml
	cd backend && go tool oapi-codegen -config oapi-codegen-client.yaml ../api/openapi.yaml
	cd backend && go tool oapi-codegen -config oapi-codegen-sim.yaml ../api/sim.openapi.yaml
	cd backend && bin/sqlc generate
	cd web && npx openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts && npx prettier --log-level warn --write src/api/schema.d.ts
	cd web && npx openapi-typescript ../api/sim.openapi.yaml -o sim/src/api/schema.d.ts && npx prettier --log-level warn --write sim/src/api/schema.d.ts

web/node_modules: web/package-lock.json
	cd web && npm ci --no-audit --no-fund
	@touch web/node_modules

GENERATED := backend/internal/httpapi/gen backend/internal/client/gen backend/internal/store/db web/src/api/schema.d.ts \
  backend/internal/sim/control/gen web/sim/src/api/schema.d.ts
.PHONY: fin
fin: ## Build the fin command line into backend/bin/fin (docs/cli.md)
	cd backend && go build -trimpath -ldflags "-X main.version=$$(git describe --tags --always --dirty)" -o bin/fin ./cmd/fin

.PHONY: gen-check
gen-check: gen ## Regenerate and fail if generated code differs from what is committed
	@# Compares with the index, so generated code that is staged for the next commit counts as clean.
	@drift="$$(git diff --name-only -- $(GENERATED); git ls-files --others --exclude-standard -- $(GENERATED))"; if [ -n "$$drift" ]; then \
	  echo "gen-check: generated code is out of date; run 'make gen' and commit:"; echo "$$drift"; git --no-pager diff --stat -- $(GENERATED); exit 1; fi
	@echo "gen-check: ok"

.PHONY: oracle
oracle: ## Run tools/legacy-oracle -> testdata/oracle/ (headless Chromium via Playwright)
	@[ -d $(ORACLE)/node_modules ] || (cd $(ORACLE) && npm ci --no-audit --no-fund)
	node $(ORACLE)/oracle.mjs

.PHONY: fixtures fixtures-check
fixtures: ## Regenerate testdata/fixtures/synthetic/ from make-fixtures.mjs
	node $(ORACLE)/make-fixtures.mjs
fixtures-check: ## Fail if the synthetic fixtures differ from the generator
	@node $(ORACLE)/make-fixtures.mjs --check

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
migrate-up: $(ENV_FILE) ## Apply migrations (finuance migrate up) against the local database
	$(RUN_ENV) cd backend && go run ./cmd/finuance migrate up
migrate-down: $(ENV_FILE) ## Roll back one migration
	$(RUN_ENV) cd backend && go run ./cmd/finuance migrate down
migrate-status: $(ENV_FILE) ## Show migration status
	$(RUN_ENV) cd backend && go run ./cmd/finuance migrate status
MIGRATIONS := backend/internal/store/migrations
migrate-new: ## Create a migration: make migrate-new name=add_accounts
	@[ -n "$(name)" ] || { echo "usage: make migrate-new name=short_description"; exit 2; }
	@last="$$(ls $(MIGRATIONS) | sed -n 's/^\([0-9]\{5\}\)_.*/\1/p' | sort | tail -1)"; \
	  f="$(MIGRATIONS)/$$(printf '%05d' $$((10#$${last:-0} + 1)))_$(name).sql"; \
	  printf -- '-- +goose Up\n\n-- +goose Down\n' > "$$f"; echo "created $$f"

##@ Running

.PHONY: up
up: $(ENV_FILE) web-dist ## Full Compose stack: db, migrate, api, worker, web (app on http://localhost:8000)
	VERSION=$(VERSION) $(COMPOSE) up -d $(UP_FLAGS) --wait

.PHONY: up-observability
up-observability: $(ENV_FILE) web-dist ## The Compose stack plus local Grafana, Prometheus and Tempo (grafana/otel-lgtm; http://localhost:3000)
	VERSION=$(VERSION) $(COMPOSE) -f deploy/compose/compose.observability.yaml up -d $(UP_FLAGS) --wait

# The lab for the traffic simulator (PLAN 25.8): a Compose project of its own next to `make up`.
LAB_COMPOSE := docker compose -p finuance-lab --env-file $(ENV_FILE) --env-file .env.lab -f deploy/compose/compose.yaml \
  -f deploy/compose/compose.observability.yaml -f deploy/compose/compose.lab.yaml
ANIMAL ?= fox

.env.lab:
	@umask 077; { echo "# The lab's secrets (make sim-up); random, for this machine's lab only"; \
	  echo "FINUANCE_SIM_SEED=$$(openssl rand -hex 32)"; echo "FINUANCE_SIM_CONTROL_TOKEN=$$(openssl rand -hex 32)"; \
	  echo "FINUANCE_LAB_ADMIN_PASSWORD=$$(openssl rand -hex 16)"; } > .env.lab
	@echo "Created .env.lab (the simulator's seed and control token, the lab administrator's password)."

# The lab's administrator (a person, not an animal): sees Administration → Developer, which links the
# synthetic users console (ADR 0044). Its password is in .env.lab.
LAB_ADMIN_EMAIL := admin@lab.invalid

.PHONY: sim-up
sim-up: $(ENV_FILE) .env.lab web-dist ## The lab: a simulation instance, the mock model, the simulator and Grafana (http://localhost:8020, :8090, :3000)
	@grep -q '^FINUANCE_LAB_ADMIN_PASSWORD=' .env.lab || { umask 077; echo "FINUANCE_LAB_ADMIN_PASSWORD=$$(openssl rand -hex 16)" >> .env.lab; }
	@# The instance key seals what an administrator enters in the lab (a connection's API key, two-step
	@# secrets): without one, saving a connection with a key is refused (issue 97). Written once, and only
	@# when neither env file sets one: .env.lab is read last, so a second key there would hide the first
	@# and make what was sealed with it unreadable.
	@grep -qs '^FINUANCE_INSTANCE_KEY=..*' $(ENV_FILE) .env.lab || { umask 077; echo "FINUANCE_INSTANCE_KEY=$$(openssl rand -base64 32)" >> .env.lab; }
	cd web && npm run --silent build:sim
	VERSION=$(VERSION) $(LAB_COMPOSE) up -d --build --wait
	@set -a; . ./.env.lab; set +a; printf '%s\n' "$$FINUANCE_LAB_ADMIN_PASSWORD" | $(LAB_COMPOSE) exec -T api /finuance admin create-user \
	  -admin -email=$(LAB_ADMIN_EMAIL) -display-name="Lab administrator" -password-stdin >/dev/null 2>&1 || true # there already
	@set -a; . ./.env.lab; set +a; echo "Lab: http://localhost:8020 · simulator: http://localhost:8090/#token=$$FINUANCE_SIM_CONTROL_TOKEN · Grafana: http://localhost:3000"
	@echo "Lab administrator (Administration → Developer links the console): $(LAB_ADMIN_EMAIL), make sim-password ANIMAL=admin"

.PHONY: sim-down
sim-down: ## Stop the lab and keep its database (SIM_RESET=1 drops the database volume as well)
	$(LAB_COMPOSE) down $(if $(SIM_RESET),--volumes,)

.PHONY: test-e2e-sim
test-e2e-sim: ## Playwright's specs of the simulator's page against the running lab (make sim-up first)
	@set -a; . ./.env.lab; set +a; cd web && E2E_SIM=1 E2E_SIM_URL=http://localhost:$${FINUANCE_SIM_PORT:-8090} \
	  E2E_SIM_TOKEN="$$FINUANCE_SIM_CONTROL_TOKEN" npx playwright test

.PHONY: sim-password
sim-password: ## An animal's e-mail address and password, to sign in to the lab as it (ANIMAL=fox; admin: the lab administrator)
	@if [ "$(ANIMAL)" = admin ]; then set -a; . ./.env.lab; set +a; printf '%s\t%s\n' "$(LAB_ADMIN_EMAIL)" "$$FINUANCE_LAB_ADMIN_PASSWORD"; \
	  else $(LAB_COMPOSE) exec -T sim /finuance-sim password $(ANIMAL); fi

.PHONY: web-dist
web-dist: web/node_modules ## Build web/dist on the host; the web image uses it instead of building in Docker
	cd web && npm run --silent build

.PHONY: down
down: $(ENV_FILE) ## Stop the full Compose stack (keeps the database volume)
	$(COMPOSE) down

.PHONY: logs
logs: $(ENV_FILE) ## Follow Compose logs
	$(COMPOSE) logs -f --tail=100

.PHONY: dev-api dev-worker dev-web
dev-api: $(ENV_FILE) ## Run the API on the host with .env (needs make db-up + migrate-up)
	$(RUN_ENV) cd backend && go run ./cmd/finuance serve
dev-worker: $(ENV_FILE) ## Run the worker on the host with .env
	$(RUN_ENV) cd backend && go run ./cmd/finuance worker
dev-web: web/node_modules ## Run the Vite dev server (proxies /api to make dev-api on :8080)
	cd web && npm run dev

.PHONY: seed-demo
seed-demo: $(ENV_FILE) ## Demo data for a user: make seed-demo email=…
	@test -n "$(email)" || { echo "usage: make seed-demo email=you@example.com"; exit 2; }
	$(RUN_ENV) cd backend && go run ./cmd/finuance seed-demo -email='$(email)'

.PHONY: llmmock
llmmock: ## Mock model server on :11434 for local UI work (OpenAI and Anthropic protocols)
	cd backend && go run ./cmd/llmmock -addr :11434

.PHONY: eval-assistant
eval-assistant: ## Assistant evals against a real model: CONNECTION= MODEL= THINK= (Docker for the DB; docs/evals.md)
	cd backend && FINUANCE_EVAL_CONNECTION=$(CONNECTION) FINUANCE_EVAL_MODEL=$(MODEL) FINUANCE_EVAL_THINK=$(THINK) \
	  FINUANCE_EVAL_MODE=$(MODE) FINUANCE_EVAL_TOOLSET=$(TOOLSET) \
	  go test -tags eval -run TestEvalAssistant -count=1 -timeout $(or $(TIMEOUT),3h) -v ./internal/assistant/eval

.PHONY: eval-receipts
eval-receipts: ## Receipt evals (images) against a real model that reads them: CONNECTION= MODEL= THINK= (docs/evals.md)
	cd backend && FINUANCE_EVAL_CONNECTION=$(CONNECTION) FINUANCE_EVAL_MODEL=$(MODEL) FINUANCE_EVAL_THINK=$(THINK) \
	  go test -tags eval -run TestEvalReceipts -count=1 -timeout 2h -v ./internal/assistant/eval

##@ Build and deploy

.PHONY: images
images: ## Build the backend, web, simulator and mock-model images locally (web/dist and web/dist-sim are used when present)
	docker build -f deploy/docker/Dockerfile.backend --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
	  --build-arg DATE=$(DATE) -t finuance-backend:dev .
	docker build -f deploy/docker/Dockerfile.web -t finuance-web:dev .
	docker build -f deploy/docker/Dockerfile.sim --build-arg VERSION=$(VERSION) -t finuance-sim:dev .
	docker build -f deploy/docker/Dockerfile.backend --target llmmock -t finuance-llmmock:dev .

.PHONY: helm-lint
helm-lint: ## Lint, render and validate the Helm charts, Argo CD manifests, dashboards and alert rules (needs helm, kubeconform, node, Docker, network)
	@tools/check-helm.sh
	@tools/check-observability.sh

.PHONY: check-terraform
check-terraform: ## The Google Cloud lab's Terraform without any cloud account: format, validity, tests on a mocked plan, no keys, a scan (needs network for the providers)
	@tools/check-terraform.sh

# The Google Cloud lab (docs/gcp.md). It is billed while it is up: `gcp-up` and `gcp-down` ask for
# the project id to be typed. Values: deploy/terraform/lab.tfvars (never committed), GCP_STATE_BUCKET.
.PHONY: gcp-plan gcp-up gcp-down gcp-verify-empty
gcp-plan: ## What `make gcp-up` would create (reads the state bucket, changes nothing)
	@tools/gcp-lab.sh plan
gcp-up: ## Create the Google Cloud lab's infrastructure (billed until gcp-down; asks for the project id)
	@tools/gcp-lab.sh up
gcp-down: ## Destroy the Google Cloud lab and everything in it (asks for the project id)
	@tools/gcp-lab.sh down
gcp-verify-empty: ## After gcp-down: list anything of the lab that is left and would cost money (needs gcloud)
	@tools/gcp-lab.sh verify-empty

.PHONY: loadtest
loadtest: ## k6 load test against the running stack (PROFILE=load|smoke|ramp, LOADTEST_BASE_URL, LOADTEST_USERS); needs make loadtest-seed
	mkdir -p loadtest/results
	@# k6 runs as this user, so the results folder needs no wider rights; the password is inherited
	@# from the environment (-e PASSWORD without a value), not written on the command line
	PASSWORD="$(LOADTEST_PASSWORD)" docker run --rm --network host --user "$$(id -u):$$(id -g)" \
	  -v "$(CURDIR)/loadtest:/scripts:ro" -v "$(CURDIR)/loadtest/results:/out" \
	  -e BASE_URL=$(LOADTEST_BASE_URL) -e USERS=$(LOADTEST_USERS) -e PASSWORD -e PROFILE=$(PROFILE) \
	  $(if $(LOADTEST_PEOPLE),,-e USERS_FILE=/scripts/users.json) \
	  $(K6_IMAGE) run --quiet --summary-export=/out/summary-$(PROFILE).json /scripts/finuance.js

.PHONY: zap
zap: ## OWASP ZAP baseline + API scan against the running stack (needs sign-ups; rules in .zap/rules.tsv, reports in zap-reports/)
	BASE_URL=$(LOADTEST_BASE_URL) tools/zap-scan.sh

.PHONY: loadtest-seed
loadtest-seed: $(ENV_FILE) ## Give LOADTEST_USERS animals about LOADTEST_ROWS transactions each on a lab (LOADTEST_PEOPLE=1: people elsewhere)
ifeq ($(LOADTEST_PEOPLE),)
	@test -s $(LOADTEST_SEED) || { umask 077; openssl rand -hex 32 > $(LOADTEST_SEED); }
	$(COMPOSE) exec -T api /finuance admin synthetic-users -count=$(LOADTEST_USERS) -seed-stdin < $(LOADTEST_SEED)
	@export FINUANCE_SIM_SEED="$$(cat $(LOADTEST_SEED))"; cd backend && \
	  go run ./cmd/finuance-sim seed -target=$(LOADTEST_BASE_URL) -count=$(LOADTEST_USERS) -rows=$(LOADTEST_ROWS) -years=$(LOADTEST_YEARS) && \
	  go run ./cmd/finuance-sim users -count=$(LOADTEST_USERS) -out=../loadtest/users.json
else
	echo "$(LOADTEST_PASSWORD)" | $(COMPOSE) exec -T api /finuance seed-demo -email=loadtest@example.test \
	  -users=$(LOADTEST_USERS) -rows=$(LOADTEST_ROWS) -password-stdin
endif
