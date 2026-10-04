# ADR-0067: voyage-4 at 1024 dimensions, and a double that runs the worker with no key.

import asyncio
import math
from types import SimpleNamespace

import pytest

from adapters.embeddings import registry, voyage
from adapters.embeddings.base import DIMENSION, EmbeddingError
from adapters.embeddings.double import DoubleEmbedder
from infra.config import Settings


async def test_the_double_is_deterministic_normalised_and_close_for_shared_words() -> None:
    double = DoubleEmbedder()
    a, b, c, empty = await double.embed(
        ["depto en Jesus Maria", "depto en Jesus Maria", "receta de budin", ""], input_type="document"
    )
    assert len(a) == DIMENSION and a == b
    assert math.isclose(sum(v * v for v in a), 1.0)
    query = (await double.embed(["cuanto sale el depto en Jesus Maria"], input_type="query"))[0]
    assert sum(x * y for x, y in zip(query, a, strict=True)) > sum(x * y for x, y in zip(query, c, strict=True))
    assert empty[0] == 1.0, "a zero vector is refused by Qdrant under cosine"


class FakeVoyage:
    last: "FakeVoyage"

    def __init__(self, **kwargs: object) -> None:
        self.built_with = kwargs
        self.calls: list[dict[str, object]] = []
        self.fail = False
        FakeVoyage.last = self

    async def embed(self, texts: list[str], **kwargs: object) -> SimpleNamespace:
        self.calls.append({"texts": texts, **kwargs})
        if self.fail:
            raise RuntimeError("rate limited")
        return SimpleNamespace(embeddings=[[0.5] * DIMENSION for _ in texts])


async def test_voyage_embeds_in_batches_with_the_model_and_dimension_of_the_adr(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(voyage.voyageai, "AsyncClient", FakeVoyage)
    embedder = voyage.VoyageEmbedder("k", 30)
    vectors = await embedder.embed([f"trozo {i}" for i in range(voyage.BATCH + 1)], input_type="document")

    sdk = FakeVoyage.last
    assert sdk.built_with == {"api_key": "k", "max_retries": 0, "timeout": 30}
    assert [len(c["texts"]) for c in sdk.calls] == [voyage.BATCH, 1]
    assert sdk.calls[0]["model"] == "voyage-4" and sdk.calls[0]["output_dimension"] == DIMENSION
    assert sdk.calls[0]["input_type"] == "document"
    assert len(vectors) == voyage.BATCH + 1 and embedder.model == "voyage-4"

    sdk.fail = True
    with pytest.raises(EmbeddingError, match="rate limited"):
        await embedder.embed(["x"], input_type="query")


async def test_a_provider_that_answers_short_is_an_error(monkeypatch: pytest.MonkeyPatch) -> None:
    class Short(FakeVoyage):
        async def embed(self, texts: list[str], **kwargs: object) -> SimpleNamespace:
            return SimpleNamespace(embeddings=[])

    monkeypatch.setattr(voyage.voyageai, "AsyncClient", Short)
    with pytest.raises(EmbeddingError, match="0 vectors for 1 texts"):
        await voyage.VoyageEmbedder("k", 30).embed(["x"], input_type="document")


# ADR-0074, regla 3: the turn waits for the whole call, whatever the SDK's own timeout does per phase.
async def test_a_provider_that_does_not_answer_in_time_is_an_error(monkeypatch: pytest.MonkeyPatch) -> None:
    class Slow(FakeVoyage):
        async def embed(self, texts: list[str], **kwargs: object) -> SimpleNamespace:
            await asyncio.sleep(1)
            return SimpleNamespace(embeddings=[[0.5] * DIMENSION for _ in texts])

    monkeypatch.setattr(voyage.voyageai, "AsyncClient", Slow)
    with pytest.raises(EmbeddingError, match="TimeoutError"):
        await voyage.VoyageEmbedder("k", 0.01).embed(["x"], input_type="query")


# The api asks with the turn's timeout, and the worker with its own.
def test_the_registry_takes_the_turns_timeout_when_it_is_given(settings: Settings, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(voyage.voyageai, "AsyncClient", FakeVoyage)
    registry._voyage.cache_clear()
    try:
        registry.resolve(settings.model_copy(update={"voyage_api_key": "k"}), timeout=5.0)
        assert FakeVoyage.last.built_with["timeout"] == 5.0
    finally:
        registry._voyage.cache_clear()


def test_the_registry_chooses_the_double_only_when_settings_says_so(settings: Settings, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(voyage.voyageai, "AsyncClient", FakeVoyage)
    assert isinstance(registry.resolve(settings.model_copy(update={"embeddings_use_double": True})), DoubleEmbedder)
    registry._voyage.cache_clear()
    try:
        real = registry.resolve(settings.model_copy(update={"voyage_api_key": "k"}))
        assert isinstance(real, voyage.VoyageEmbedder)
        assert registry.resolve(settings.model_copy(update={"voyage_api_key": "k"})) is real
    finally:
        registry._voyage.cache_clear()
