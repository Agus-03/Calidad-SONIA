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


@dataclass(frozen=True)
class SearchRules:
    # What pipeline_steps.version of the retrieval and grounding steps writes (ADR-0068, regla 5).
    version: int
    top_k: int


@lru_cache(maxsize=1)
def rules() -> SearchRules:
    loaded = rules_file.load("retrieval.yaml")
    return SearchRules(version=loaded.version, top_k=loaded.integer("top_k", minimum=1))


@dataclass(frozen=True)
class Found:
    chunk_id: UUID
    document_id: UUID
    content: str
    # What retrieval_logs stores: similarity_score, and rank counted from 1 in the order the search returned them.
    similarity: float
    rank: int


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
    # skips retrieval and grounding without writing their rows. An empty tuple is a search that ran and found nothing, which is
    # retrieval's outcome empty. The provider's and Qdrant's failures are raised: what the turn does with them is the pipeline's.
    async def search(self, agent_id: UUID, text: str) -> tuple[Found, ...] | None:
        async with self._connection() as conn:
            base = await repository.base(conn, agent_id, model=self._embedder.model)
        if base is None:
            return None
        if base.stale_document_ids:
            logger.warning(
                "documents embedded by another model are left out of the search",
                extra={
                    "context": {
                        "agent_id": str(agent_id),
                        "model": self._embedder.model,
                        "documents": [str(d) for d in base.stale_document_ids],
                    }
                },
            )
        # Nothing comparable to search, or nothing to search with: the question is not even sent to the provider.
        if not base.document_ids or not text.strip():
            return ()

        (vector,) = await self._embedder.embed([text], input_type="query")
        hits = await self._vectors.search(
            base.collection_name, vector, limit=self.rules.top_k, document_ids=base.document_ids
        )
        if not hits:
            return ()

        async with self._connection() as conn:
            chunks = await repository.chunks(conn, [hit.id for hit in hits])
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
        if len(found) < len(hits):
            # A point whose chunk retired or whose document left indexed between the two reads: rare, and it costs a place.
            logger.info(
                "points without a live chunk were dropped from the search",
                extra={"context": {"agent_id": str(agent_id), "dropped": len(hits) - len(found)}},
            )
        return tuple(found)
