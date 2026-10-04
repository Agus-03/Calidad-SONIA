# ADR-0068, reglas 3 and 10, and ADR-0073: the question embedded as a query, the top_k of the agent's indexed documents, every
# chunk with its similarity and its rank, what the search left out on the way, and no search at all for an agent that does not
# retrieve.

import logging
from collections.abc import AsyncGenerator, Sequence
from contextlib import asynccontextmanager
from uuid import UUID, uuid4

import pytest

from adapters.vectorstore.qdrant import Hit
from infra.rules import RulesFile, RulesFileError
from knowledge import repository, search
from knowledge.search import Found, Searcher, SearchResult, SearchRules

RULES = SearchRules(version=1, top_k=3)


class SpyEmbedder:
    model = "voyage-4"

    def __init__(self) -> None:
        self.calls: list[tuple[list[str], str]] = []

    async def embed(self, texts: Sequence[str], *, input_type: str) -> list[list[float]]:
        self.calls.append((list(texts), input_type))
        return [[0.5, 0.5] for _ in texts]


class FakeVectors:
    def __init__(self, hits: list[Hit]) -> None:
        self.hits = hits
        self.calls: list[dict[str, object]] = []

    async def search(self, name: str, vector: list[float], *, limit: int, document_ids: Sequence[UUID]) -> list[Hit]:
        self.calls.append({"name": name, "vector": vector, "limit": limit, "document_ids": list(document_ids)})
        return self.hits


@asynccontextmanager
async def no_database() -> AsyncGenerator[object]:
    yield object()


def searcher(embedder: SpyEmbedder, vectors: FakeVectors) -> Searcher:
    return Searcher(embedder=embedder, vectors=vectors, connection=no_database, rules=RULES)


# The two reads, doubled: what base() is asked with and what chunks() is asked with are recorded, so a test can say the model
# and the documents are checked again on the second read.
def with_base(
    monkeypatch: pytest.MonkeyPatch, found: repository.Base | None, chunks: dict[UUID, repository.Chunk]
) -> list[dict[str, object]]:
    asked: list[dict[str, object]] = []

    async def base(conn: object, agent_id: UUID, *, model: str) -> repository.Base | None:
        asked.append({"read": "base", "model": model})
        return found

    async def read_chunks(
        conn: object, ids: Sequence[UUID], *, model: str, document_ids: Sequence[UUID]
    ) -> dict[UUID, repository.Chunk]:
        asked.append({"read": "chunks", "model": model, "document_ids": list(document_ids)})
        return {i: chunks[i] for i in ids if i in chunks}

    monkeypatch.setattr(repository, "base", base)
    monkeypatch.setattr(repository, "chunks", read_chunks)
    return asked


def test_the_rules_start_at_the_top_k_the_group_chose() -> None:
    search.rules.cache_clear()
    assert search.rules() == SearchRules(version=1, top_k=3)


def test_a_top_k_retrieval_logs_could_not_rank_stops_the_brain(monkeypatch: pytest.MonkeyPatch) -> None:
    raw = {"top_k": search.MAX_TOP_K + 1}
    monkeypatch.setattr(search.rules_file, "load", lambda name: RulesFile(name=name, version=1, raw=raw))
    search.rules.cache_clear()
    try:
        with pytest.raises(RulesFileError, match="at most"):
            search.rules()
    finally:
        search.rules.cache_clear()


async def test_an_agent_without_indexed_documents_does_not_retrieve(monkeypatch: pytest.MonkeyPatch) -> None:
    with_base(monkeypatch, None, {})
    embedder, vectors = SpyEmbedder(), FakeVectors([])

    assert await searcher(embedder, vectors).search(uuid4(), "cuanto sale el depto") is None
    assert embedder.calls == [] and vectors.calls == [], "no question leaves for the provider"


async def test_the_question_goes_as_a_query_and_the_chunks_come_back_in_order_with_their_rank(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    document = uuid4()
    first, second, third = uuid4(), uuid4(), uuid4()
    chunks = {
        first: repository.Chunk(first, document, "Alquiler: $320.000 + expensas."),
        second: repository.Chunk(second, document, "Depto 2 ambientes en Jesus Maria."),
        third: repository.Chunk(third, document, "Requisitos: recibo de sueldo."),
    }
    asked = with_base(monkeypatch, repository.Base("kb_agente", (document,), ()), chunks)
    embedder = SpyEmbedder()
    vectors = FakeVectors([Hit(first, 0.81), Hit(second, 0.64), Hit(third, 0.40)])

    result = await searcher(embedder, vectors).search(uuid4(), "Busca depto. ¿Y cuanto sale?")

    assert embedder.calls == [(["Busca depto. ¿Y cuanto sale?"], "query")], "Voyage embeds a question differently"
    assert vectors.calls == [{"name": "kb_agente", "vector": [0.5, 0.5], "limit": 3, "document_ids": [document]}]
    assert asked == [
        {"read": "base", "model": "voyage-4"},
        {"read": "chunks", "model": "voyage-4", "document_ids": [document]},
    ], "the model and the documents are checked again on the second read"
    assert result == SearchResult(
        found=(
            Found(first, document, "Alquiler: $320.000 + expensas.", 0.81, 1),
            Found(second, document, "Depto 2 ambientes en Jesus Maria.", 0.64, 2),
            Found(third, document, "Requisitos: recibo de sueldo.", 0.40, 3),
        )
    ), "no threshold here: the grounding applies it"


async def test_a_point_whose_chunk_is_no_longer_live_is_dropped_counted_and_the_ranks_stay_contiguous(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    document, live, retired = uuid4(), uuid4(), uuid4()
    with_base(monkeypatch, repository.Base("kb", (document,), ()), {live: repository.Chunk(live, document, "vigente")})
    vectors = FakeVectors([Hit(retired, 0.9), Hit(live, 0.7)])

    result = await searcher(SpyEmbedder(), vectors).search(uuid4(), "pregunta")

    assert result == SearchResult(found=(Found(live, document, "vigente", 0.7, 1),), dropped=1)


async def test_documents_of_another_model_are_left_out_returned_and_reported(
    monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
) -> None:
    current, stale = uuid4(), uuid4()
    with_base(monkeypatch, repository.Base("kb", (current,), (stale,)), {})
    vectors = FakeVectors([])

    with caplog.at_level(logging.WARNING, logger="sonia.knowledge"):
        result = await searcher(SpyEmbedder(), vectors).search(uuid4(), "pregunta")

    assert result == SearchResult(found=(), stale_document_ids=(stale,)), "the measurement can tell it from a miss"
    assert vectors.calls[0]["document_ids"] == [current]
    (record,) = caplog.records
    assert "another model" in record.getMessage() and record.context["documents"] == 1


async def test_a_base_with_nothing_comparable_or_a_blank_question_searches_nothing(monkeypatch: pytest.MonkeyPatch) -> None:
    embedder, vectors = SpyEmbedder(), FakeVectors([Hit(uuid4(), 0.9)])
    stale = uuid4()

    with_base(monkeypatch, repository.Base("kb", (), (stale,)), {})
    assert await searcher(embedder, vectors).search(uuid4(), "pregunta") == SearchResult(found=(), stale_document_ids=(stale,))

    with_base(monkeypatch, repository.Base("kb", (uuid4(),), ()), {})
    assert await searcher(embedder, vectors).search(uuid4(), "   \n ") == SearchResult(found=())

    assert embedder.calls == [] and vectors.calls == []


async def test_a_search_that_finds_no_point_returns_empty(monkeypatch: pytest.MonkeyPatch) -> None:
    with_base(monkeypatch, repository.Base("kb", (uuid4(),), ()), {})
    assert await searcher(SpyEmbedder(), FakeVectors([])).search(uuid4(), "pregunta") == SearchResult(found=())
