# The healthcheck of docker-compose and the CI read this endpoint, so it has to be able to say no.

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


def test_create_app_mounts_health(settings: Settings) -> None:
    assert list(create_app(settings).openapi()["paths"]) == ["/health", "/turns"]
