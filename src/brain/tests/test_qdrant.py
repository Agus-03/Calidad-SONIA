# ADR-0010, ADR-0064 regla 3, ADR-0067 regla 2: one collection per agent at 1024 and cosine, and only document_id in a point.

import json
from uuid import uuid4

import httpx
import pytest

from adapters.vectorstore.qdrant import Hit, Point, QdrantStore, VectorStoreError


def store_with(handler) -> tuple[QdrantStore, list[httpx.Request]]:
    seen: list[httpx.Request] = []

    def record(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return handler(request)

    return QdrantStore("http://qdrant:6333/", "clave", 5, transport=httpx.MockTransport(record)), seen


async def test_a_missing_collection_is_created_with_the_dimension_and_distance_of_the_adr() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(404, json={"status": {"error": "Not found"}})
        return httpx.Response(200, json={"result": True})

    store, seen = store_with(handler)
    await store.ensure_collection("kb_agente")
    await store.close()

    assert [(r.method, r.url.path) for r in seen] == [("GET", "/collections/kb_agente"), ("PUT", "/collections/kb_agente")]
    assert json.loads(seen[1].content) == {"vectors": {"size": 1024, "distance": "Cosine"}}
    assert seen[0].headers["api-key"] == "clave"


async def test_an_existing_collection_is_left_as_it_is() -> None:
    store, seen = store_with(lambda request: httpx.Response(200, json={"result": {}}))
    await store.ensure_collection("kb_agente")
    assert len(seen) == 1


async def test_points_carry_the_chunk_id_and_nothing_but_the_document() -> None:
    store, seen = store_with(lambda request: httpx.Response(200, json={"result": {}}))
    chunk, document = uuid4(), uuid4()

    await store.upsert("kb_agente", [Point(id=chunk, vector=[0.1, 0.2], document_id=document)])
    await store.upsert("kb_agente", [])
    await store.delete_document("kb_agente", document)

    upsert, delete = seen
    assert upsert.url.path == "/collections/kb_agente/points" and upsert.url.params["wait"] == "true"
    assert json.loads(upsert.content) == {
        "points": [{"id": str(chunk), "vector": [0.1, 0.2], "payload": {"document_id": str(document)}}]
    }
    assert delete.url.path == "/collections/kb_agente/points/delete"
    assert json.loads(delete.content) == {"filter": {"must": [{"key": "document_id", "match": {"value": str(document)}}]}}


async def test_a_long_document_goes_up_in_batches_in_order() -> None:
    seen: list[httpx.Request] = []

    def record(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(200, json={"result": {}})

    store = QdrantStore("http://qdrant:6333", "", 5, batch_size=2, transport=httpx.MockTransport(record))
    document = uuid4()
    points = [Point(id=uuid4(), vector=[float(i)], document_id=document) for i in range(5)]
    await store.upsert("kb_agente", points)

    sent = [[p["id"] for p in json.loads(r.content)["points"]] for r in seen]
    assert [len(batch) for batch in sent] == [2, 2, 1]
    assert [i for batch in sent for i in batch] == [str(p.id) for p in points]
    assert all(r.url.params["wait"] == "true" for r in seen)


async def test_a_search_asks_for_the_limit_among_the_named_documents_only() -> None:
    chunk = uuid4()
    documents = [uuid4(), uuid4()]
    answer = {"result": {"points": [{"id": str(chunk), "version": 3, "score": 0.82}]}, "status": "ok"}
    store, seen = store_with(lambda request: httpx.Response(200, json=answer))

    hits = await store.search("kb_agente", [0.1, 0.2], limit=3, document_ids=documents)

    assert hits == [Hit(id=chunk, score=0.82)]
    (request,) = seen
    assert (request.method, request.url.path) == ("POST", "/collections/kb_agente/points/query")
    assert json.loads(request.content) == {
        "query": [0.1, 0.2],
        "limit": 3,
        "with_payload": False,
        "filter": {"must": [{"key": "document_id", "match": {"any": [str(d) for d in documents]}}]},
    }


async def test_a_search_with_nothing_to_search_finds_nothing() -> None:
    store, seen = store_with(lambda request: httpx.Response(404, json={}))
    assert await store.search("kb_agente", [1.0], limit=3, document_ids=[]) == []
    assert seen == [], "no document to search is not even a request"
    assert await store.search("kb_nunca_indexada", [1.0], limit=3, document_ids=[uuid4()]) == []


async def test_deleting_from_a_collection_that_does_not_exist_is_nothing_to_do() -> None:
    store, _ = store_with(lambda request: httpx.Response(404, json={}))
    await store.delete_document("kb_nunca_indexada", uuid4())


async def test_a_failure_of_qdrant_is_its_own_error() -> None:
    store, _ = store_with(lambda request: httpx.Response(500, text="boom"))
    with pytest.raises(VectorStoreError, match="500"):
        await store.upsert("kb", [Point(id=uuid4(), vector=[1.0], document_id=uuid4())])

    def unreachable(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused")

    store, _ = store_with(unreachable)
    with pytest.raises(VectorStoreError, match="ConnectError"):
        await store.ensure_collection("kb")
