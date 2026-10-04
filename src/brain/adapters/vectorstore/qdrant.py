# Qdrant through its REST API (ADR-0010, ADR-0071): one collection per AIAgent, cosine distance, and only document_id in each
# point's payload, because the text is read from document_chunks and Qdrant keeps no copy of it (ADR-0064, regla 3).

from collections.abc import Sequence
from dataclasses import dataclass
from typing import Any
from uuid import UUID

import httpx

from adapters.embeddings.base import DIMENSION


class VectorStoreError(Exception):
    pass


@dataclass(frozen=True)
class Point:
    # document_chunks.id, which vector_id stores: Qdrant takes a UUID as a point id, so there is no second id to keep in step.
    id: UUID
    vector: list[float]
    document_id: UUID


@dataclass(frozen=True)
class Hit:
    # The chunk's id, since the point's id is the chunk's (ADR-0064, regla 5).
    id: UUID
    # Cosine similarity, the higher the closer: what retrieval_logs.similarity_score stores and the grounding thresholds.
    score: float


class QdrantStore:
    def __init__(
        self,
        url: str,
        api_key: str,
        timeout: float,
        *,
        batch_size: int = 128,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        self._batch_size = batch_size
        headers = {"api-key": api_key} if api_key else {}
        self._http = httpx.AsyncClient(base_url=url.rstrip("/"), headers=headers, timeout=timeout, transport=transport)

    async def close(self) -> None:
        await self._http.aclose()

    # Created by the worker with the first document of a base, so a base nobody indexed has no collection (ADR-0064, regla 5).
    async def ensure_collection(self, name: str) -> None:
        found = await self._call("GET", f"/collections/{name}", allow_missing=True)
        if found is None:
            await self._call("PUT", f"/collections/{name}", json={"vectors": {"size": DIMENSION, "distance": "Cosine"}})
            schema: dict[str, Any] = {}
        else:
            schema = found["result"].get("payload_schema") or {}
        # The search filters by document_id on every turn (ADR-0073): without an index Qdrant checks the filter point by point,
        # which is slower and can cost a filtered search some of its true neighbours. A collection made before the index gets it
        # with its next document.
        if "document_id" not in schema:
            await self._call(
                "PUT", f"/collections/{name}/index?wait=true", json={"field_name": "document_id", "field_schema": "keyword"}
            )

    # wait=true so the points are searchable when this returns, and the document can be marked indexed (ADR-0064, regla 7 (iv)).
    # In batches, so a long document stays under Qdrant's request size and the timeout: a point is about 20 KB of JSON. A batch
    # that fails leaves the earlier ones up, and the reindex deletes them by document_id before going up again (regla 7 (i)).
    async def upsert(self, name: str, points: Sequence[Point]) -> None:
        for start in range(0, len(points), self._batch_size):
            batch = points[start : start + self._batch_size]
            body = {"points": [{"id": str(p.id), "vector": p.vector, "payload": {"document_id": str(p.document_id)}} for p in batch]}
            await self._call("PUT", f"/collections/{name}/points?wait=true", json=body)

    # By document and not by point, so the vectors of a document go whole, whatever chunks the database still lists for it.
    # A collection that does not exist has nothing to delete, which is the base whose first document failed.
    async def delete_document(self, name: str, document_id: UUID) -> None:
        body = {"filter": {"must": [{"key": "document_id", "match": {"value": str(document_id)}}]}}
        await self._call("POST", f"/collections/{name}/points/delete?wait=true", json=body, allow_missing=True)

    # The closest points among the documents the caller names, and only those: a document that is not indexed, or that the sweep
    # has not cleaned yet, takes no place among the limit (ADR-0068, regla 3). No payload comes back, because the text is read
    # from document_chunks. A collection that does not exist is an error and not an empty answer: the worker creates it before
    # it marks any document indexed, so a missing one means Qdrant lost what the database says it holds (ADR-0073, nota).
    async def search(self, name: str, vector: list[float], *, limit: int, document_ids: Sequence[UUID]) -> list[Hit]:
        if not document_ids:
            return []
        body = {
            "query": vector,
            "limit": limit,
            "with_payload": False,
            "filter": {"must": [{"key": "document_id", "match": {"any": [str(d) for d in document_ids]}}]},
        }
        found = await self._call("POST", f"/collections/{name}/points/query", json=body)
        return [Hit(id=UUID(str(point["id"])), score=float(point["score"])) for point in found["result"]["points"]]

    async def _call(self, method: str, path: str, *, json: Any = None, allow_missing: bool = False) -> Any | None:
        try:
            response = await self._http.request(method, path, json=json)
        except httpx.HTTPError as err:
            raise VectorStoreError(f"qdrant: {method} {path}: {type(err).__name__}: {err}") from err
        if allow_missing and response.status_code == 404:
            return None
        if response.is_error:
            raise VectorStoreError(f"qdrant: {method} {path}: {response.status_code} {response.text[:500]}")
        return response.json()
