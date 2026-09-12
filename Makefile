# Every command that touches the stack, so the --env-file of the root .env is written once and not remembered.

COMPOSE := docker compose --env-file .env -f deploy/docker-compose.yml

# The same project plus the development tooling, so pgadmin lands on the network the database is already on.
COMPOSE_DEV := $(COMPOSE) -f deploy/docker-compose.dev.yml

# The same three services with both ends doubled, which is what lets the whole path run without a key and without a WABA.
COMPOSE_SMOKE := $(COMPOSE) -f deploy/docker-compose.smoke.yml

# Read so the integration DSN is assembled here and nobody has to type a password into a shell.
-include .env

.DEFAULT_GOAL := help
.PHONY: help up down logs build test test-core test-brain test-integration smoke contracts contracts-check migrate migrate-status seed seed-admin pgadmin pgadmin-down dbhub dbhub-down

help:
	@echo "up         levanta la base, el nucleo y el cerebro"
	@echo "down       los baja y borra los contenedores"
	@echo "logs       sigue la salida de los tres servicios"
	@echo "build      reconstruye las dos imagenes"
	@echo "test       corre los tests de los dos servicios, sin exigir el umbral"
	@echo "test-integration  los mismos, mas los que necesitan la base, y exige el 80% de ADR-0028"
	@echo "smoke      levanta el stack doblado y recorre el camino entero: webhook, respuesta y evidencia"
	@echo "contracts  regenera las enumeraciones de Go y de Python"
	@echo "contracts-check   falla si lo generado quedo viejo"
	@echo "migrate    aplica el esquema levantando el nucleo, que migra al arrancar"
	@echo "migrate-status    lista lo que la base dice que ya corrio"
	@echo "seed       carga los datos de demo: agente, inbox y contacto fijos"
	@echo "seed-admin crea la cuenta y su primer admin, con lo que dice SEED_ADMIN_* del .env"
	@echo "pgadmin    abre la base en el navegador, en localhost:5050"
	@echo "pgadmin-down      lo baja sin tocar los tres servicios"
	@echo "dbhub      levanta el MCP de PostgreSQL de .mcp.json, de solo lectura, en localhost:8090"
	@echo "dbhub-down        lo baja sin tocar los tres servicios"

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

build:
	$(COMPOSE) build

# Each service is tested with its own tooling, because ADR-0028 measures coverage per service and never aggregates.
test: test-core test-brain

# Reports the number without judging it: with no database the SQL half of the core never runs, and the figure would refuse a healthy tree.
test-core:
	cd src/core && go test -coverprofile=coverage.out ./... && grep -v -E "enums_gen.go|/src/core/main.go" coverage.out > coverage.filtered.out && go tool cover -func=coverage.filtered.out | awk '/^total:/ { gsub("%", "", $$3); printf "core coverage %s%%\n", $$3 }'

# The threshold is in pyproject.toml, so running pytest by hand enforces the same number the CI does.
test-brain:
	cd src/brain && poetry run pytest

# Run through the brain venv because that is where pyyaml and FastAPI live; the generator belongs to neither service.
contracts:
	cd src/brain && poetry run python ../../contracts/generate.py

# A generated file only drifts when a contract changed and nobody regenerated it: the enumerations and the OpenAPI.
contracts-check:
	cd src/brain && poetry run python ../../contracts/generate.py --check

# There is no separate migration step: the core applies the schema when it boots, so this is the whole mechanism.
migrate:
	$(COMPOSE) up -d --build db core

# Reads the runner's own bookkeeping, which is the only place that knows what actually ran.
migrate-status:
	$(COMPOSE) exec -T db psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -c "SELECT version, applied_at FROM schema_migrations ORDER BY version"

# Never run by the application at boot (regla 3): demo data is a decision someone takes, not a side effect of starting.
seed:
	$(COMPOSE) exec -T db psql -v ON_ERROR_STOP=1 -U $(POSTGRES_USER) -d $(POSTGRES_DB) < src/core/seeds/0001_demo.sql

# Idempotent, and the password lives only in the environment: a second run never resets one that was changed since (ADR-0030).
seed-admin:
	$(COMPOSE) exec -T core core --seed-admin

# Brings the database with it, because pgadmin against nothing is an empty tree.
pgadmin:
	$(COMPOSE_DEV) up -d db pgadmin
	@echo "pgadmin en http://localhost:5050 - el servidor SONIA ya esta en el arbol"

pgadmin-down:
	$(COMPOSE_DEV) rm -sf pgadmin

# Brings the database with it for the same reason as pgadmin: without it every MCP query fails on connect.
dbhub:
	$(COMPOSE_DEV) up -d db dbhub
	@echo "MCP de PostgreSQL en http://localhost:8090/mcp - solo lectura, contra la base de desarrollo"

dbhub-down:
	$(COMPOSE_DEV) rm -sf dbhub

# The same suites with the variable that turns the skipped tests on, so the stack has to be up first.
test-integration: export SONIA_TEST_DATABASE_URL := postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/$(POSTGRES_DB)
# The 80% of ADR-0028 is enforced here and nowhere else: this is the only target where every line of the core actually gets a chance to run.
test-integration: test
	cd src/core && go tool cover -func=coverage.filtered.out | awk '/^total:/ { gsub("%", "", $$3); if ($$3 + 0 < 80) { printf "core coverage %s%% is below the 80%% of ADR-0028\n", $$3; exit 1 } }'

# -count=1 so a cached pass cannot call a dead stack green, and --wait blocks until both healthchecks actually answer.
smoke: export SONIA_TEST_DATABASE_URL := postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/$(POSTGRES_DB)
smoke: export SONIA_SMOKE_CORE_URL := http://localhost:8080
smoke: export SONIA_SMOKE_BRAIN_URL := http://localhost:8000
# The core's own key, so the smoke can encrypt the demo inbox's app secret and sign its webhooks with it (ADR-0031, F20).
smoke: export SONIA_SMOKE_ENCRYPTION_KEY := $(ENCRYPTION_KEY)
smoke:
	$(COMPOSE_SMOKE) up -d --build --wait
	$(MAKE) seed
	cd src/core && go test ./smoke/... -count=1 -v
