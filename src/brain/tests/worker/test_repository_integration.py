# ADR-0064, reglas 7 a 9, and ADR-0065, regla 9, against the real DDL: the claim, the chunks with what made them, the two ends of
# a document, the stale lock and the removed document whose vectors are still live. One transaction, rolled back.

import os
from uuid import UUID

import pytest

from infra import ids
from infra.db import Database
from worker import repository

_DSN = os.getenv("SONIA_TEST_DATABASE_URL", "")

pytestmark = pytest.mark.skipif(
    not _DSN,
    reason="SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration",
)


async def _seed(conn) -> tuple[UUID, str]:
    cursor = await conn.execute("SELECT id FROM accounts LIMIT 1")
    account = await cursor.fetchone()
    if account is None:
        pytest.skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
    operator, agent, base = ids.new(), ids.new(), ids.new()
    await conn.execute(
        """INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
           VALUES (%s, %s, 'Prueba', 'Worker', %s, 'x', 'admin', true)""",
        (operator, account["id"], f"{operator}@sonia.test"),
    )
    await conn.execute(
        """INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
           VALUES (%s, %s, %s, 'autonomous', 'anthropic', 'claude-sonnet-5-5')""",
        (agent, account["id"], f"Worker {agent}"),
    )
    collection = f"kb_{agent}"
    await conn.execute(
        "INSERT INTO knowledge_bases (id, ai_agent_id, collection_name) VALUES (%s, %s, %s)", (base, agent, collection)
    )
    # Nothing else may be pending, or the claim of the case could take a document that is not its own.
    await conn.execute("UPDATE documents SET status = 'failed' WHERE status = 'pending'")
    return operator, base, collection


async def _document(conn, operator: UUID, base: UUID) -> UUID:
    document = ids.new()
    await conn.execute(
        """INSERT INTO documents (id, knowledge_base_id, filename, mime_type, size_bytes, checksum, object_key, status,
                                  uploaded_by_operator_id, personal_data_terms_version)
           VALUES (%s, %s, 'precios.pdf', 'application/pdf', 10, %s, %s, 'pending', %s, 1)""",
        (document, base, str(document), f"knowledge-bases/{base}/{document}", operator),
    )
    return document


async def test_the_worker_writes_what_adr_0064_gives_it_and_nothing_else() -> None:
    database = Database(_DSN)
    await database.open()
    try:
        async with database.connection() as conn, conn.transaction(force_rollback=True):
            operator, base, collection = await _seed(conn)
            document = await _document(conn, operator, base)

            claimed = await repository.claim(conn)
            assert claimed == repository.Claimed(id=document, object_key=f"knowledge-bases/{base}/{document}", collection_name=collection)
            assert await repository.claim(conn) is None, "a processing document is not taken twice"

            first = repository.Chunk(ids.new(), 0, "Depto 2 ambientes.", 5, 1, None, "double")
            await repository.replace_chunks(conn, document, [first])
            second = repository.Chunk(ids.new(), 0, "Depto 2 ambientes, renovado.", 7, 1, 1, "voyage-4")
            await repository.replace_chunks(conn, document, [second])
            cursor = await conn.execute(
                """SELECT id, vector_id, deleted_at IS NOT NULL AS retired, chunking_version, description_prompt_version,
                          embedding_model, token_count
                   FROM document_chunks WHERE document_id = %s ORDER BY created_at, id""",
                (document,),
            )
            rows = {row["id"]: row for row in await cursor.fetchall()}
            assert rows[first.id]["retired"] and rows[first.id]["vector_id"] is None, "the old chunk is marked, not deleted"
            live = rows[second.id]
            assert not live["retired"] and live["vector_id"] == str(second.id)
            assert (live["chunking_version"], live["description_prompt_version"], live["embedding_model"]) == (1, 1, "voyage-4")

            assert await repository.mark_indexed(conn, document) is True
            assert await repository.mark_indexed(conn, document) is False, "only a processing document is finished"
            cursor = await conn.execute("SELECT status, locked_at, error_message FROM documents WHERE id = %s", (document,))
            assert await cursor.fetchone() == {"status": "indexed", "locked_at": None, "error_message": None}

            failing = await _document(conn, operator, base)
            assert (await repository.claim(conn)).id == failing
            assert await repository.mark_failed(conn, failing, "x" * 900) is True
            cursor = await conn.execute("SELECT status, error_message FROM documents WHERE id = %s", (failing,))
            row = await cursor.fetchone()
            assert row["status"] == "failed" and len(row["error_message"]) == 500

            stale = await _document(conn, operator, base)
            await repository.claim(conn)
            await conn.execute("UPDATE documents SET locked_at = now() - interval '2 hours' WHERE id = %s", (stale,))
            assert await repository.release_stale(conn, timeout_seconds=900) == [stale]
            assert await repository.release_stale(conn, timeout_seconds=900) == []

            await conn.execute("UPDATE documents SET deleted_at = now() WHERE id = %s", (document,))
            assert await repository.removed_with_live_chunks(conn) == [repository.Removed(id=document, collection_name=collection)]
            await repository.retire_chunks(conn, document)
            assert await repository.removed_with_live_chunks(conn) == []
    finally:
        await database.close()
