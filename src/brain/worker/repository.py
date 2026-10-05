# What the worker reads and writes: documents.status, locked_at and error_message, which ADR-0019 gives it, and document_chunks,
# an evidence table (regla 3). The core owns every other column of documents and the whole of knowledge_bases.

from collections.abc import Sequence
from dataclasses import dataclass
from uuid import UUID

from psycopg import AsyncConnection

from infra.enums import DocumentStatus


@dataclass(frozen=True)
class Claimed:
    id: UUID
    object_key: str
    collection_name: str


@dataclass(frozen=True)
class Chunk:
    id: UUID
    index: int
    content: str
    token_count: int
    # The three that say what made it (ADR-0065, regla 9). description_prompt_version is None when no image was described.
    chunking_version: int
    description_prompt_version: int | None
    embedding_model: str


@dataclass(frozen=True)
class Removed:
    id: UUID
    collection_name: str


# One document at a time, the oldest first. SKIP LOCKED so a second worker would take the next one instead of waiting, and the
# status written in the same statement that chose it, so nothing else can take it in between (ADR-0064, regla 7).
_CLAIM = """
UPDATE documents d
SET    status = %(processing)s, locked_at = now(), error_message = NULL, updated_at = now()
FROM   knowledge_bases kb
WHERE  kb.id = d.knowledge_base_id
  AND  d.id = (SELECT id FROM documents
               WHERE  status = %(pending)s AND deleted_at IS NULL
               ORDER  BY created_at, id
               LIMIT  1
               FOR UPDATE SKIP LOCKED)
RETURNING d.id, d.object_key, kb.collection_name
"""


async def claim(conn: AsyncConnection) -> Claimed | None:
    cursor = await conn.execute(_CLAIM, {"processing": DocumentStatus.PROCESSING, "pending": DocumentStatus.PENDING})
    row = await cursor.fetchone()
    if row is None:
        return None
    return Claimed(id=row["id"], object_key=row["object_key"], collection_name=row["collection_name"])


# ADR-0064, regla 8: a document held in processing longer than the timeout belonged to a worker that died, and goes back.
_RELEASE_STALE = """
UPDATE documents
SET    status = %(pending)s, locked_at = NULL, updated_at = now()
WHERE  status = %(processing)s AND locked_at < now() - make_interval(secs => %(seconds)s)
RETURNING id
"""


async def release_stale(conn: AsyncConnection, *, timeout_seconds: float) -> list[UUID]:
    cursor = await conn.execute(
        _RELEASE_STALE,
        {"pending": DocumentStatus.PENDING, "processing": DocumentStatus.PROCESSING, "seconds": timeout_seconds},
    )
    return [row["id"] for row in await cursor.fetchall()]


# The old chunks are marked and not deleted: retrieval_logs points at them, and they are the evidence of a past measurement of
# OE6 (DER/relaciones.md §9). vector_id goes to NULL because their vectors are already gone from Qdrant (ADR-0064, regla 7 (ii)).
_RETIRE_CHUNKS = """
UPDATE document_chunks
SET    vector_id = NULL, deleted_at = now()
WHERE  document_id = %(document_id)s AND deleted_at IS NULL
"""

_INSERT_CHUNK = """
INSERT INTO document_chunks (id, document_id, chunk_index, content, token_count, vector_id,
                             chunking_version, description_prompt_version, embedding_model)
VALUES (%(id)s, %(document_id)s, %(index)s, %(content)s, %(token_count)s, %(vector_id)s,
        %(chunking_version)s, %(description_prompt_version)s, %(embedding_model)s)
"""


# Takes the connection so the caller owns the transaction: the old ones retire and the new ones enter together (regla 5).
async def replace_chunks(conn: AsyncConnection, document_id: UUID, chunks: Sequence[Chunk]) -> None:
    await conn.execute(_RETIRE_CHUNKS, {"document_id": document_id})
    for chunk in chunks:
        await conn.execute(
            _INSERT_CHUNK,
            {
                "id": chunk.id,
                "document_id": document_id,
                "index": chunk.index,
                "content": chunk.content,
                "token_count": chunk.token_count,
                # The point's id is the chunk's (ADR-0064, regla 5), so vector_id is known before the point exists.
                "vector_id": str(chunk.id),
                "chunking_version": chunk.chunking_version,
                "description_prompt_version": chunk.description_prompt_version,
                "embedding_model": chunk.embedding_model,
            },
        )


async def retire_chunks(conn: AsyncConnection, document_id: UUID) -> None:
    await conn.execute(_RETIRE_CHUNKS, {"document_id": document_id})


# Guarded on processing, so a document the sweep sent back to pending while this worker was slow is not marked over.
_FINISH = """
UPDATE documents
SET    status = %(status)s, locked_at = NULL, error_message = %(error)s, updated_at = now()
WHERE  id = %(id)s AND status = %(processing)s
"""


async def mark_indexed(conn: AsyncConnection, document_id: UUID) -> bool:
    cursor = await conn.execute(
        _FINISH,
        {"status": DocumentStatus.INDEXED, "error": None, "id": document_id, "processing": DocumentStatus.PROCESSING},
    )
    return cursor.rowcount == 1


# The reason is what the admin reads in the list (ADR-0064), so it is cut to a length a row can show.
async def mark_failed(conn: AsyncConnection, document_id: UUID, reason: str) -> bool:
    cursor = await conn.execute(
        _FINISH,
        {"status": DocumentStatus.FAILED, "error": reason[:500], "id": document_id, "processing": DocumentStatus.PROCESSING},
    )
    return cursor.rowcount == 1


# ADR-0064, regla 9: a document the core removed whose chunks are still live has vectors that can still be found.
_REMOVED = """
SELECT DISTINCT d.id, kb.collection_name
FROM   documents d
JOIN   knowledge_bases kb ON kb.id = d.knowledge_base_id
JOIN   document_chunks c  ON c.document_id = d.id AND c.deleted_at IS NULL
WHERE  d.deleted_at IS NOT NULL
"""


async def removed_with_live_chunks(conn: AsyncConnection) -> list[Removed]:
    cursor = await conn.execute(_REMOVED)
    return [Removed(id=row["id"], collection_name=row["collection_name"]) for row in await cursor.fetchall()]
