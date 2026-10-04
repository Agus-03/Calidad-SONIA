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


class QdrantStore:
    def __init__(self, url: str, api_key: str, timeout: float, *, transport: httpx.AsyncBaseTransport | None = None) -> None:
        headers = {"api-key": api_key} if api_key else {}
        self._http = httpx.AsyncClient(base_url=url.rstrip("/"), headers=headers, timeout=timeout, transport=transport)

    async def close(self) -> None:
        await self._http.aclose()

    # Created by the worker with the first document of a base, so a base nobody indexed has no collection (ADR-0064, regla 5).
    async def ensure_collection(self, name: str) -> None:
        found = await self._call("GET", f"/collections/{name}", allow_missing=True)
        if found is not None:
            return
        await self._call("PUT", f"/collections/{name}", json={"vectors": {"size": DIMENSION, "distance": "Cosine"}})

    # wait=true so the points are searchable when this returns, and the document can be marked indexed (ADR-0064, regla 7 (iv)).
    async def upsert(self, name: str, points: Sequence[Point]) -> None:
        if not points:
            return
        body = {"points": [{"id": str(p.id), "vector": p.vector, "payload": {"document_id": str(p.document_id)}} for p in points]}
        await self._call("PUT", f"/collections/{name}/points?wait=true", json=body)

    # By document and not by point, so the vectors of a document go whole, whatever chunks the database still lists for it.
    # A collection that does not exist has nothing to delete, which is the base whose first document failed.
    async def delete_document(self, name: str, document_id: UUID) -> None:
        body = {"filter": {"must": [{"key": "document_id", "match": {"value": str(document_id)}}]}}
        await self._call("POST", f"/collections/{name}/points/delete?wait=true", json=body, allow_missing=True)

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
