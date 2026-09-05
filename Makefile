# Every command that touches the stack, so the --env-file of the root .env is written once and not remembered.

COMPOSE := docker compose --env-file .env -f deploy/docker-compose.yml

.DEFAULT_GOAL := help
.PHONY: help up down logs build test test-core test-brain

help:
	@echo "up         levanta la base, el nucleo y el cerebro"
	@echo "down       los baja y borra los contenedores"
	@echo "logs       sigue la salida de los tres servicios"
	@echo "build      reconstruye las dos imagenes"
	@echo "test       corre los tests de los dos servicios"

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
