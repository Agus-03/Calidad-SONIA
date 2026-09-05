# What the unit tests cannot reach: a live PostgreSQL behind the pool, and the lifespan that opens it.

import os

import pytest
from fastapi.testclient import TestClient

from infra.config import Settings
from infra.db import Database
from main import create_app

_DSN = os.getenv("SONIA_TEST_DATABASE_URL", "")

# Skips instead of failing, so pytest keeps working on a machine with no stack up.
pytestmark = pytest.mark.skipif(
    not _DSN,
    reason="SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration",
)


async def test_the_pool_opens_and_the_connection_answers() -> None:
    database = Database(_DSN)
    await database.open()

    try:
        async with database.connection() as conn:
            cursor = await conn.execute("SELECT 1 AS one")
            row = await cursor.fetchone()

        assert row["one"] == 1
        assert await database.ping() is True
    finally:
        await database.close()


# The lifespan is what the container runs, and nothing in the unit tests ever enters it.
def test_the_lifespan_opens_the_pool_and_health_answers_ok() -> None:
    settings = Settings(_env_file=None, database_url=_DSN, brain_internal_token="test-token")

    with TestClient(create_app(settings)) as client:
        response = client.get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
