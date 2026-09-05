# Every command that touches the stack, so the --env-file of the root .env is written once and not remembered.

COMPOSE := docker compose --env-file .env -f deploy/docker-compose.yml

# Read so the integration DSN is assembled here and nobody has to type a password into a shell.
-include .env

.DEFAULT_GOAL := help
.PHONY: help up down logs build test test-core test-brain test-integration contracts contracts-check

help:
	@echo "up         levanta la base, el nucleo y el cerebro"
	@echo "down       los baja y borra los contenedores"
	@echo "logs       sigue la salida de los tres servicios"
	@echo "build      reconstruye las dos imagenes"
	@echo "test       corre los tests de los dos servicios"
	@echo "test-integration  los mismos, mas los que necesitan la base levantada"
	@echo "contracts  regenera las enumeraciones de Go y de Python"
	@echo "contracts-check   falla si lo generado quedo viejo"

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

test-core:
	cd src/core && go test ./...

test-brain:
	cd src/brain && poetry run pytest

# Run through the brain venv because that is where pyyaml is declared; the generator belongs to neither service.
contracts:
	cd src/brain && poetry run python ../../contracts/generate.py

# The only way the three copies of an enumeration drift is a contract that changed and was never regenerated.
contracts-check:
	cd src/brain && poetry run python ../../contracts/generate.py --check

# The same suites with the variable that turns the skipped tests on, so the stack has to be up first.
test-integration: export SONIA_TEST_DATABASE_URL := postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/$(POSTGRES_DB)
test-integration: test
