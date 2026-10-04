# What the search reads: knowledge_bases and documents, which the core owns, and document_chunks, which the worker writes. Reads
# only: the search writes nothing, and the rows of retrieval_logs are the pipeline's (ADR-0068, regla 6).

from collections.abc import Sequence
from dataclasses import dataclass
from uuid import UUID

from psycopg import AsyncConnection

from infra.enums import DocumentStatus


@dataclass(frozen=True)
class Base:
    collection_name: str
    # The documents the search may find: indexed, not removed, and embedded by the model that embeds the question.
    document_ids: tuple[UUID, ...]
    # Indexed documents with live chunks from another model, left out because their vectors are not comparable with the
    # question's. A model changed without reindexing (ADR-0064, regla 11), which the search reports instead of hiding.
    stale_document_ids: tuple[UUID, ...]


@dataclass(frozen=True)
class Chunk:
    id: UUID
    document_id: UUID
    content: str


# ADR-0068, regla 10: an agent with no base, or with no document in indexed, does not retrieve. Only indexed documents, so a
# document being reprocessed or one that failed after its vectors went up is never found (ADR-0073, regla 2).
_BASE = """
SELECT kb.collection_name, d.id AS document_id,
       EXISTS (SELECT 1 FROM document_chunks c
               WHERE  c.document_id = d.id AND c.deleted_at IS NULL AND c.embedding_model <> %(model)s) AS stale
FROM   knowledge_bases kb
JOIN   documents d ON d.knowledge_base_id = kb.id
WHERE  kb.ai_agent_id = %(agent_id)s AND kb.deleted_at IS NULL
  AND  d.status = %(indexed)s AND d.deleted_at IS NULL
ORDER  BY d.created_at, d.id
"""


async def base(conn: AsyncConnection, agent_id: UUID, *, model: str) -> Base | None:
    cursor = await conn.execute(_BASE, {"agent_id": agent_id, "indexed": DocumentStatus.INDEXED, "model": model})
    rows = await cursor.fetchall()
    if not rows:
        return None
    return Base(
        # One base per agent (DER/relaciones.md §4: ai_agents 1 — 0..1 knowledge_bases), so every row names the same collection.
        collection_name=rows[0]["collection_name"],
        document_ids=tuple(row["document_id"] for row in rows if not row["stale"]),
        stale_document_ids=tuple(row["document_id"] for row in rows if row["stale"]),
    )


# The text of the chunks Qdrant found, checked again against the database: a chunk retired, or a document removed or sent back to
# pending between the two reads, is not returned (ADR-0068, regla 3: only active chunks). The model and the documents are checked
# again too, so a document reindexed by another model in between is not returned with a similarity that means nothing.
_CHUNKS = """
SELECT c.id, c.document_id, c.content
FROM   document_chunks c
JOIN   documents d ON d.id = c.document_id
WHERE  c.id = ANY(%(ids)s) AND c.deleted_at IS NULL AND c.embedding_model = %(model)s
  AND  c.document_id = ANY(%(document_ids)s)
  AND  d.status = %(indexed)s AND d.deleted_at IS NULL
"""


async def chunks(
    conn: AsyncConnection, ids: Sequence[UUID], *, model: str, document_ids: Sequence[UUID]
) -> dict[UUID, Chunk]:
    cursor = await conn.execute(
        _CHUNKS,
        {"ids": list(ids), "model": model, "document_ids": list(document_ids), "indexed": DocumentStatus.INDEXED},
    )
    return {
        row["id"]: Chunk(id=row["id"], document_id=row["document_id"], content=row["content"])
        for row in await cursor.fetchall()
    }
