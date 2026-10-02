# What the unit tests cannot reach: a live PostgreSQL behind the pool, and the lifespan that opens it.

import asyncio
import os
from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from infra.config import Settings
from infra.db import Database
from main import create_app
from prompts.repository import QuickReply, read_quick_replies

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

    # The client runs its own loop and ignores the policy of conftest.py, and psycopg async cannot run on the Windows default.
    with TestClient(create_app(settings), backend_options={"loop_factory": asyncio.SelectorEventLoop}) as client:
        response = client.get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


# F07 against the real DDL: the agent's account, only the active and not deleted, by title in any casing (ADR-0061).
async def test_the_quick_replies_are_read_as_the_operators_see_them() -> None:
    database = Database(_DSN)
    await database.open()
    try:
        async with database.connection() as conn, conn.transaction(force_rollback=True):
            cursor = await conn.execute("SELECT id, account_id FROM ai_agents WHERE deleted_at IS NULL LIMIT 1")
            agent = await cursor.fetchone()
            if agent is None:
                pytest.skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
            # The brain writes no quick replies; the test does, inside a transaction that is always rolled back.
            admin, token = uuid4(), uuid4().hex
            await conn.execute(
                "INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)"
                " VALUES (%s, %s, 'Prueba', 'Cerebro', %s, 'not-a-real-hash', 'admin', true)",
                (admin, agent["account_id"], f"{admin}@sonia.test"),
            )
            catalogue = (("b horarios", True, False), ("A precios", True, False), ("pausada", False, False), ("borrada", True, True))
            for title, active, deleted in catalogue:
                await conn.execute(
                    "INSERT INTO quick_replies (id, account_id, title, content, created_by_operator_id, active, deleted_at)"
                    " VALUES (%s, %s, %s, %s, %s, %s, CASE WHEN %s THEN now() END)",
                    (uuid4(), agent["account_id"], f"{title} {token}", f"texto de {title}", admin, active, deleted),
                )

            replies = await read_quick_replies(conn, agent["id"], 100)
            mine = [r for r in replies if r.title.endswith(token)]

            assert mine == [
                QuickReply(f"A precios {token}", "texto de A precios"),
                QuickReply(f"b horarios {token}", "texto de b horarios"),
            ]
    finally:
        await database.close()
