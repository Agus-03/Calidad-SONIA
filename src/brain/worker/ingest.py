# The ingestion of one document (ADR-0064, regla 7): it is taken, read and cut outside any transaction, and then written in the
# order that keeps PostgreSQL ahead of Qdrant and the old ahead of the new.

import logging
from collections.abc import Callable
from contextlib import AbstractAsyncContextManager
from dataclasses import dataclass
from typing import Protocol
from uuid import UUID

from psycopg import AsyncConnection

from adapters.embeddings.base import Embedder
from adapters.vectorstore.qdrant import Point
from infra import ids
from knowledge import chunking
from worker import repository
from worker.images import DescriptionRules, Describer, worth_describing
from worker.pdf import Page, read

logger = logging.getLogger("sonia.worker.ingest")


# What makes a document fail with a reason the admin can read, as opposed to a bug, which fails it with the exception's name.
class IngestionError(Exception):
    pass


class Objects(Protocol):
    async def download(self, key: str) -> bytes: ...


class Opener(Protocol):
    def open(self, sealed: bytes) -> bytes: ...


class Vectors(Protocol):
    async def ensure_collection(self, name: str) -> None: ...

    async def upsert(self, name: str, points: list[Point]) -> None: ...

    async def delete_document(self, name: str, document_id: UUID) -> None: ...


@dataclass(frozen=True)
class Parts:
    objects: Objects
    opener: Opener
    embedder: Embedder
    vectors: Vectors
    describer: Describer
    description_rules: DescriptionRules
    chunking_rules: chunking.ChunkingRules
    # One connection per write, each its own transaction, so no transaction is held across a download or an embedding.
    connection: Callable[[], AbstractAsyncContextManager[AsyncConnection]]


class Ingestor:
    def __init__(self, parts: Parts) -> None:
        self._p = parts

    # Takes the next pending document and ingests it. Answers whether there was one, so the loop drains a backlog before sleeping.
    async def next(self) -> bool:
        async with self._p.connection() as conn:
            claimed = await repository.claim(conn)
        if claimed is None:
            return False

        try:
            await self._ingest(claimed)
        except Exception as err:  # noqa: BLE001 - any failure is the document's, and has to leave it failed and not processing
            reason = str(err) if isinstance(err, IngestionError) else f"{type(err).__name__}: {err}"
            logger.warning("document failed", extra={"context": {"document_id": str(claimed.id), "error": reason}})
            async with self._p.connection() as conn:
                await repository.mark_failed(conn, claimed.id, reason)
        return True

    # ADR-0064, reglas 8 y 9: what a dead worker left in processing goes back, and what the core removed loses its vectors.
    async def sweep(self, *, lock_timeout_seconds: float) -> None:
        async with self._p.connection() as conn:
            released = await repository.release_stale(conn, timeout_seconds=lock_timeout_seconds)
        for document_id in released:
            logger.warning("stale document released", extra={"context": {"document_id": str(document_id)}})

        async with self._p.connection() as conn:
            removed = await repository.removed_with_live_chunks(conn)
        for document in removed:
            # Qdrant first and the chunks after: a failure in between leaves the chunks live and recoverable, never marked
            # gone while their vectors are still searchable (DER/entidades.md, document_chunks).
            await self._p.vectors.delete_document(document.collection_name, document.id)
            async with self._p.connection() as conn:
                await repository.retire_chunks(conn, document.id)
            logger.info("removed document unindexed", extra={"context": {"document_id": str(document.id)}})

    async def _ingest(self, claimed: repository.Claimed) -> None:
        sealed = await self._p.objects.download(claimed.object_key)
        pdf = self._p.opener.open(sealed)
        sizes = self._p.description_rules
        pages = read(
            pdf,
            max_side=sizes.max_side,
            mode=self._p.chunking_rules.pdf_text,
            layout_min_ratio=self._p.chunking_rules.layout_min_ratio,
            min_width=sizes.min_width,
            min_height=sizes.min_height,
        )
        # ADR-0064, regla 10: no OCR, so a PDF without extractable text is failed before anything is paid for its images.
        if not any(page.text.strip() for page in pages):
            raise IngestionError("el PDF no tiene texto extraíble (sin OCR)")

        text, described = await self._text(pages)
        pieces = chunking.split(text, rules=self._p.chunking_rules)
        if not pieces:
            raise IngestionError("el PDF no tiene texto extraíble (sin OCR)")
        vectors = await self._p.embedder.embed(pieces, input_type="document")

        chunks = [
            repository.Chunk(
                id=ids.new(),
                index=index,
                content=piece,
                token_count=self._p.chunking_rules.estimate_tokens(piece),
                chunking_version=self._p.chunking_rules.version,
                description_prompt_version=self._p.describer.version if described else None,
                embedding_model=self._p.embedder.model,
            )
            for index, piece in enumerate(pieces)
        ]

        # (i) the previous vectors of this document leave Qdrant; (ii) its previous chunks retire and the new ones enter, in one
        # transaction; (iii) the new vectors go up; (iv) the document is indexed. A failure after (ii) leaves chunks whose
        # vectors are not there yet, under a document that is failed and gets reindexed whole (ADR-0064, regla 7).
        await self._p.vectors.ensure_collection(claimed.collection_name)
        await self._p.vectors.delete_document(claimed.collection_name, claimed.id)
        async with self._p.connection() as conn, conn.transaction():
            await repository.replace_chunks(conn, claimed.id, chunks)
        await self._p.vectors.upsert(
            claimed.collection_name,
            [Point(id=chunk.id, vector=vector, document_id=claimed.id) for chunk, vector in zip(chunks, vectors, strict=True)],
        )
        async with self._p.connection() as conn:
            indexed = await repository.mark_indexed(conn, claimed.id)
        logger.info(
            "document indexed" if indexed else "document indexed after the sweep released it",
            extra={
                "context": {
                    "document_id": str(claimed.id),
                    "chunks": len(chunks),
                    "images_described": described,
                    # The pages the layout mode would have lost text on, read plain instead (ADR-0076, regla 4): the only place
                    # that says which documents mix the two modes under one chunking_version.
                    "pages_read_plain": sum(page.read_plain for page in pages),
                }
            },
        )

    # The text of the document with each image's description in the page it was on, so the chunking keeps the two together
    # (ADR-0067, regla 5). Pages are joined with one blank line, the chunking's paragraph separator: a page break weighs what a
    # paragraph does, and less than the wide vertical gap inside a page that the layout mode writes as two blank lines.
    async def _text(self, pages: list[Page]) -> tuple[str, int]:
        described = 0
        out: list[str] = []
        for page in pages:
            parts = [page.text.strip()] if page.text.strip() else []
            for image in page.images:
                if not worth_describing(image, self._p.description_rules):
                    continue
                description = await self._p.describer.describe(image, page=page.number)
                if description is not None:
                    parts.append(f"[Imagen: {description}]")
                    described += 1
            if parts:
                out.append("\n".join(parts))
        return "\n\n".join(out), described
