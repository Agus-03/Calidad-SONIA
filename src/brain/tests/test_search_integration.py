# ADR-0068, reglas 3 and 10, and ADR-0073 against the real DDL and a real Qdrant: only the chunks of indexed documents embedded by
# the model of the question come back, in Qdrant's order, and an agent with no base does not retrieve. The rows are rolled back
# and the collection is dropped.

import os
from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager
from uuid import UUID

import httpx
import pytest

from adapters.embeddings.double import DoubleEmbedder
from adapters.vectorstore.qdrant import Point, QdrantStore, VectorStoreError
from infra import ids
from infra.db import Database
from knowledge import search
from knowledge.search import Searcher

_DSN = os.getenv("SONIA_TEST_DATABASE_URL", "")
_QDRANT = os.getenv("SONIA_TEST_QDRANT_URL", "")

pytestmark = pytest.mark.skipif(
    not (_DSN and _QDRANT),
    reason="SONIA_TEST_DATABASE_URL or SONIA_TEST_QDRANT_URL is not set: bring the stack up and run make test-integration",
)


async def _agent(conn) -> tuple[UUID, UUID]:
    cursor = await conn.execute("SELECT id FROM accounts LIMIT 1")
    account = await cursor.fetchone()
    if account is None:
        pytest.skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
    operator, agent = ids.new(), ids.new()
    await conn.execute(
        """INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
           VALUES (%s, %s, 'Prueba', 'Busqueda', %s, 'x', 'admin', true)""",
        (operator, account["id"], f"{operator}@sonia.test"),
    )
    await conn.execute(
        """INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
           VALUES (%s, %s, %s, 'autonomous', 'anthropic', 'claude-sonnet-5-5')""",
        (agent, account["id"], f"Busqueda {agent}"),
    )
    return operator, agent


async def _document(conn, base: UUID, operator: UUID, status: str, *, removed: bool = False) -> UUID:
    document = ids.new()
    await conn.execute(
        """INSERT INTO documents (id, knowledge_base_id, filename, mime_type, size_bytes, checksum, object_key, status,
                                  uploaded_by_operator_id, personal_data_terms_version, deleted_at)
           VALUES (%s, %s, 'precios.pdf', 'application/pdf', 10, %s, %s, %s, %s, 1, CASE WHEN %s THEN now() END)""",
        (document, base, str(document), f"knowledge-bases/{base}/{document}", status, operator, removed),
    )
    return document


async def _chunk(conn, document: UUID, content: str, model: str) -> UUID:
    chunk = ids.new()
    await conn.execute(
        """INSERT INTO document_chunks (id, document_id, chunk_index, content, token_count, vector_id, chunking_version,
                                        embedding_model)
           VALUES (%s, %s, 0, %s, 10, %s, 1, %s)""",
        (chunk, document, content, str(chunk), model),
    )
    return chunk


async def test_only_the_live_chunks_of_indexed_documents_of_the_same_model_are_found() -> None:
    database = Database(_DSN)
    await database.open()
    vectors = QdrantStore(_QDRANT, "", 10)
    embedder = DoubleEmbedder()
    collection = ""
    try:
        async with database.connection() as conn, conn.transaction(force_rollback=True):

            @asynccontextmanager
            async def same_transaction() -> AsyncGenerator:
                yield conn

            operator, agent = await _agent(conn)
            base = ids.new()
            collection = f"kb_{agent}"
            await conn.execute(
                "INSERT INTO knowledge_bases (id, ai_agent_id, collection_name) VALUES (%s, %s, %s)", (base, agent, collection)
            )

            indexed = await _document(conn, base, operator, "indexed")
            price = await _chunk(conn, indexed, "Alquiler del depto en Jesus Maria: $320.000 + expensas.", embedder.model)
            rooms = await _chunk(conn, indexed, "El depto tiene 2 ambientes y balcon.", embedder.model)
            # Each of these is closer to the question than the indexed chunks, and none of them may come back.
            text = "Precio del alquiler del depto en Jesus Maria: $320.000 + expensas."
            pending = await _chunk(conn, await _document(conn, base, operator, "pending"), text, embedder.model)
            removed = await _chunk(conn, await _document(conn, base, operator, "indexed", removed=True), text, embedder.model)
            stale_document = await _document(conn, base, operator, "indexed")
            stale = await _chunk(conn, stale_document, text, "voyage-4")

            # Every chunk gets its point, as the worker would have left them before the document changed status.
            await vectors.ensure_collection(collection)
            await vectors.ensure_collection(collection)
            async with httpx.AsyncClient(base_url=_QDRANT) as client:
                schema = (await client.get(f"/collections/{collection}")).json()["result"]["payload_schema"]
            assert schema["document_id"]["data_type"] == "keyword", "the filter of every search has its index"
            cursor = await conn.execute(
                """SELECT c.id, c.document_id, c.content FROM document_chunks c
                   JOIN documents d ON d.id = c.document_id WHERE d.knowledge_base_id = %s""",
                (base,),
            )
            rows = await cursor.fetchall()
            embedded = await embedder.embed([row["content"] for row in rows], input_type="document")
            await vectors.upsert(
                collection,
                [Point(id=row["id"], vector=v, document_id=row["document_id"]) for row, v in zip(rows, embedded, strict=True)],
            )

            searcher = Searcher(
                embedder=embedder, vectors=vectors, connection=same_transaction, rules=search.rules()
            )
            result = await searcher.search(agent, "¿Cuanto sale el alquiler del depto en Jesus Maria?")

            assert result is not None
            assert result.stale_document_ids == (stale_document,) and result.dropped == 0
            found = result.found
            assert [f.chunk_id for f in found] == [price, rooms], "the closest indexed chunk first, and nothing else"
            assert not {pending, removed, stale} & {f.chunk_id for f in found}
            assert [f.rank for f in found] == [1, 2]
            assert found[0].similarity > found[1].similarity > 0
            assert found[0].document_id == indexed and found[0].content.startswith("Alquiler")

            _, without_base = await _agent(conn)
            assert await searcher.search(without_base, "¿Cuanto sale?") is None, "an agent with no base does not retrieve"

            # Qdrant lost the collection the database says holds indexed documents: an error, not a search that found nothing.
            with pytest.raises(VectorStoreError, match="404"):
                await vectors.search(f"kb_{ids.new()}", [1.0] * 1024, limit=3, document_ids=[indexed])
    finally:
        if collection:
            async with httpx.AsyncClient(base_url=_QDRANT) as client:
                await client.delete(f"/collections/{collection}")
        await vectors.close()
        await database.close()
