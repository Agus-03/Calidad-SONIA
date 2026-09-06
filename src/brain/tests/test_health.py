# The healthcheck of docker-compose and the CI read this endpoint, so it has to be able to say no.

import re

from fastapi import FastAPI
from fastapi.testclient import TestClient

from api.routes import health
from infra.config import Settings
from main import create_app


class _StubDatabase:
    def __init__(self, answers: bool) -> None:
        self._answers = answers

    async def ping(self) -> bool:
        return self._answers


def _client(answers: bool) -> TestClient:
    app = FastAPI()
    app.include_router(health.router)
    app.state.database = _StubDatabase(answers)
    return TestClient(app)


def test_a_reachable_database_answers_ok() -> None:
    response = _client(True).get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


# A check that always says ok cannot fail a deploy, which is the only thing it is for.
def test_an_unreachable_database_answers_service_unavailable() -> None:
    response = _client(False).get("/health")

    assert response.status_code == 503
    assert response.json() == {"status": "unavailable"}


# Quantified over the routes instead of listing them: a new route extends this test rather than breaking it, and the
# failure it catches is the one that matters, which is mounting an endpoint outside the internal router by accident.
def test_only_health_answers_without_the_shared_secret(settings: Settings) -> None:
    app = create_app(settings)
    app.state.database = _StubDatabase(True)
    client = TestClient(app)

    paths = app.openapi()["paths"]
    assert "/health" in paths, "the healthcheck of docker-compose and the CI has to stay mounted"

    for path, operations in paths.items():
        for method in operations:
            # A path parameter has to be filled for the route to match at all; the token is checked before the handler.
            url = re.sub(r"\{[^}]+\}", "00000000-0000-0000-0000-000000000000", path)
            status = client.request(method, url).status_code

            if path == "/health":
                assert status != 401, "the container healthcheck cannot be asked for a secret"
            else:
                assert status == 401, f"{method.upper()} {path} answers without the shared secret"
