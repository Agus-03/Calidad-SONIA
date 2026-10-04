# The semantic search of F06 (ADR-0068, ADR-0073): the question embedded as a query, the top_k closest chunks of the agent's
# collection, and every one of them returned with its similarity and its position. No threshold here: the grounding applies it,
# and retrieval logs everything the search found so the threshold can be swept later (ADR-0068, reglas 3 and 4).

import logging
from collections.abc import Callable, Sequence
from contextlib import AbstractAsyncContextManager
from dataclasses import dataclass
from functools import lru_cache
from typing import Protocol
from uuid import UUID

from psycopg import AsyncConnection

from adapters.embeddings.base import Embedder
from adapters.vectorstore.qdrant import Hit
from infra import rules as rules_file
from knowledge import repository

logger = logging.getLogger("sonia.knowledge")

# The largest rank retrieval_logs.rank can hold, a smallint: a top_k above it could not be logged.
MAX_TOP_K = 32767


@dataclass(frozen=True)
class SearchRules:
    # What pipeline_steps.version of the retrieval and grounding steps writes (ADR-0068, regla 5).
    version: int
    top_k: int


@lru_cache(maxsize=1)
def rules() -> SearchRules:
    loaded = rules_file.load("retrieval.yaml")
    return SearchRules(version=loaded.version, top_k=loaded.integer("top_k", minimum=1, maximum=MAX_TOP_K))


@dataclass(frozen=True)
class Found:
    chunk_id: UUID
    document_id: UUID
    content: str
    # What retrieval_logs stores: similarity_score, and rank counted from 1 in the order the search returned them.
    similarity: float
    rank: int


# What a search that ran returns: the chunks, and what it left out on the way, so the retrieval step can write it in its
# detail_json and a measurement can tell a question the documents do not answer from a search that could not look (ADR-0073, nota).
@dataclass(frozen=True)
class SearchResult:
    found: tuple[Found, ...]
    # Indexed documents with live chunks from another embedding model, left out because they are not comparable.
    stale_document_ids: tuple[UUID, ...] = ()
    # Points Qdrant returned whose chunk was no longer live when the text was read: places of the top_k that came back empty.
    dropped: int = 0


class Vectors(Protocol):
    async def search(self, name: str, vector: list[float], *, limit: int, document_ids: Sequence[UUID]) -> list[Hit]: ...


class Searcher:
    def __init__(
        self,
        *,
        embedder: Embedder,
        vectors: Vectors,
        connection: Callable[[], AbstractAsyncContextManager[AsyncConnection]],
        rules: SearchRules,
    ) -> None:
        self._embedder = embedder
        self._vectors = vectors
        self._connection = connection
        self.rules = rules

    # None when the agent does not retrieve at all (ADR-0068, regla 10): no base, or no document in indexed. The pipeline then
    # skips retrieval and grounding without writing their rows. A result with nothing found is a search that ran, which is
    # retrieval's outcome empty. The provider's and Qdrant's failures are raised, a missing collection among them: what the turn
    # does with them is the pipeline's.
    async def search(self, agent_id: UUID, text: str) -> SearchResult | None:
        model = self._embedder.model
        async with self._connection() as conn:
            base = await repository.base(conn, agent_id, model=model)
        if base is None:
            return None
        stale = base.stale_document_ids
        if stale:
            logger.warning(
                "documents embedded by another model are left out of the search",
                extra={"context": {"agent_id": str(agent_id), "model": model, "documents": len(stale)}},
            )
        # Nothing comparable to search, or nothing to search with: the question is not even sent to the provider.
        if not base.document_ids or not text.strip():
            return SearchResult(found=(), stale_document_ids=stale)

        (vector,) = await self._embedder.embed([text], input_type="query")
        hits = await self._vectors.search(
            base.collection_name, vector, limit=self.rules.top_k, document_ids=base.document_ids
        )
        if not hits:
            return SearchResult(found=(), stale_document_ids=stale)

        async with self._connection() as conn:
            chunks = await repository.chunks(conn, [hit.id for hit in hits], model=model, document_ids=base.document_ids)
        found: list[Found] = []
        for hit in hits:
            chunk = chunks.get(hit.id)
            if chunk is None:
                continue
            found.append(
                Found(
                    chunk_id=chunk.id,
                    document_id=chunk.document_id,
                    content=chunk.content,
                    similarity=hit.score,
                    rank=len(found) + 1,
                )
            )
        return SearchResult(found=tuple(found), stale_document_ids=stale, dropped=len(hits) - len(found))
