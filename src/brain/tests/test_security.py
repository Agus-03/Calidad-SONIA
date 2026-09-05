# ADR-0017 (k): the shared secret is the only thing between the brain and anyone else inside the network.

import pytest
from fastapi import APIRouter, Depends, FastAPI
from fastapi.testclient import TestClient

from api.security import require_internal_token
from infra.config import Settings


@pytest.fixture
def client(settings: Settings) -> TestClient:
    app = FastAPI()
    app.state.settings = settings

    guarded = APIRouter(dependencies=[Depends(require_internal_token)])

    @guarded.get("/protegido")
    async def _protegido() -> dict[str, str]:
        return {"status": "ok"}

    app.include_router(guarded)
    return TestClient(app)


@pytest.mark.parametrize(
    ("headers", "expected"),
    [
        ({}, 401),
        ({"X-Internal-Token": ""}, 401),
        ({"X-Internal-Token": "otro-token"}, 401),
        ({"X-Internal-Token": "test-token"}, 200),
    ],
)
def test_only_the_shared_secret_gets_through(client: TestClient, headers: dict, expected: int) -> None:
    assert client.get("/protegido", headers=headers).status_code == expected
