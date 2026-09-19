# The list the core validates an agent against (ADR-0038): it has to be rules/pricing.yaml and nothing else.

from contextlib import asynccontextmanager

from fastapi.testclient import TestClient

from infra.config import Settings
from main import create_app
from pipeline.generation import pricing


@asynccontextmanager
async def _no_lifespan(_app):
    yield


# No database behind it: the list is a versioned file, and a route that needs no pool must not open one.
def client(settings: Settings) -> TestClient:
    app = create_app(settings)
    app.router.lifespan_context = _no_lifespan
    return TestClient(app)


def test_models_rejects_a_caller_that_is_not_the_core(settings: Settings) -> None:
    with client(settings) as http:
        response = http.get("/models")

    assert response.status_code == 401


# Every priced pair and only those: a model the list offers and the turn refuses is the silent failure this route exists to prevent.
def test_models_lists_exactly_what_the_turn_can_price(settings: Settings) -> None:
    with client(settings) as http:
        response = http.get("/models", headers={"X-Internal-Token": "test-token"})

    assert response.status_code == 200
    payload = response.json()
    assert payload["pricing_version"] == pricing().version
    listed = {(m["provider"], m["model"]) for m in payload["models"]}
    assert listed == set(pricing().rates)


# Sorted so two calls answer the same body: the admin panel draws the list in the same order every time.
def test_models_answers_in_a_stable_order(settings: Settings) -> None:
    with client(settings) as http:
        payload = http.get("/models", headers={"X-Internal-Token": "test-token"}).json()

    pairs = [(m["provider"], m["model"]) for m in payload["models"]]
    assert pairs == sorted(pairs)
