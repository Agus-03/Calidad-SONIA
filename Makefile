# Every command that touches the stack, so the --env-file of the root .env is written once and not remembered.

COMPOSE := docker compose --env-file .env -f deploy/docker-compose.yml

# The same project plus the development tooling, so pgadmin lands on the network the database is already on.
COMPOSE_DEV := $(COMPOSE) -f deploy/docker-compose.dev.yml

# Read so the integration DSN is assembled here and nobody has to type a password into a shell.
-include .env

.DEFAULT_GOAL := help
.PHONY: help up down logs build test test-core test-brain test-integration contracts contracts-check migrate migrate-status seed pgadmin pgadmin-down

help:
	@echo "up         levanta la base, el nucleo y el cerebro"
	@echo "down       los baja y borra los contenedores"
	@echo "logs       sigue la salida de los tres servicios"
	@echo "build      reconstruye las dos imagenes"
	@echo "test       corre los tests de los dos servicios"
	@echo "test-integration  los mismos, mas los que necesitan la base levantada"
	@echo "contracts  regenera las enumeraciones de Go y de Python"
	@echo "contracts-check   falla si lo generado quedo viejo"
	@echo "migrate    aplica el esquema levantando el nucleo, que migra al arrancar"
	@echo "migrate-status    lista lo que la base dice que ya corrio"
	@echo "seed       carga los datos de demo: agente, inbox y contacto fijos"
	@echo "pgadmin    abre la base en el navegador, en localhost:5050"
	@echo "pgadmin-down      lo baja sin tocar los tres servicios"

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

# Go carries no threshold of its own, so the 80% of ADR-0028 is enforced here and not in the workflow.
test-core:
	cd src/core && go test -coverprofile=coverage.out ./... && grep -v -E "enums_gen.go|/src/core/main.go" coverage.out > coverage.filtered.out && go tool cover -func=coverage.filtered.out | awk '/^total:/ { gsub("%", "", $$3); printf "core coverage %s%%\n", $$3; if ($$3 + 0 < 80) { print "below the 80% of ADR-0028"; exit 1 } }'

# The threshold is in pyproject.toml, so running pytest by hand enforces the same number the CI does.
test-brain:
	cd src/brain && poetry run pytest

# Run through the brain venv because that is where pyyaml and FastAPI live; the generator belongs to neither service.
# Writes the three: the two enumeration files and the brain's OpenAPI, which is dumped from the app itself.
contracts:
	cd src/brain && poetry run python ../../contracts/generate.py

# The only way a generated file drifts is a contract that changed and was never regenerated: the enumerations in
# their three copies, and the OpenAPI against the routes and schemas the brain actually serves.
contracts-check:
	cd src/brain && poetry run python ../../contracts/generate.py --check

# There is no separate migration step: the core applies the schema when it boots, so this is the whole mechanism.
migrate:
	$(COMPOSE) up -d --build db core

# Reads the runner's own bookkeeping, which is the only place that knows what actually ran.
migrate-status:
	$(COMPOSE) exec -T db psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -c "SELECT version, applied_at FROM schema_migrations ORDER BY version"

# Never run by the application at boot (regla 3): demo data is a decision someone takes, not a side effect of starting.
# Idempotent, so running it twice is not an error and a half-seeded database can be finished by running it again.
seed:
	$(COMPOSE) exec -T db psql -v ON_ERROR_STOP=1 -U $(POSTGRES_USER) -d $(POSTGRES_DB) < src/core/seeds/0001_demo.sql

# Brings the database with it, because pgadmin against nothing is an empty tree.
pgadmin:
	$(COMPOSE_DEV) up -d db pgadmin
	@echo "pgadmin en http://localhost:5050 - el servidor SONIA ya esta en el arbol"

pgadmin-down:
	$(COMPOSE_DEV) rm -sf pgadmin

# The same suites with the variable that turns the skipped tests on, so the stack has to be up first.
test-integration: export SONIA_TEST_DATABASE_URL := postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/$(POSTGRES_DB)
test-integration: test
