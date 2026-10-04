# voyage-4 of Voyage AI (ADR-0067, regla 1). Nothing outside this file imports the voyageai SDK.

from collections.abc import Sequence
from typing import Any

import voyageai

from adapters.embeddings.base import DIMENSION, EmbeddingError, InputType

MODEL = "voyage-4"

# Well under Voyage's limits on texts and tokens per request for a batch of chunks of about 500 tokens (ADR-0065, regla 8).
BATCH = 64


class VoyageEmbedder:
    model = MODEL

    def __init__(self, api_key: str, timeout: float) -> None:
        # No retries in the SDK: a failed document goes back to failed and is retried by reindexing, never twice inside one pass.
        self._client = voyageai.AsyncClient(api_key=api_key, max_retries=0, timeout=timeout)

    async def embed(self, texts: Sequence[str], *, input_type: InputType) -> list[list[float]]:
        vectors: list[list[float]] = []
        for start in range(0, len(texts), BATCH):
            batch = list(texts[start : start + BATCH])
            try:
                result: Any = await self._client.embed(batch, model=MODEL, input_type=input_type, output_dimension=DIMENSION)
            except Exception as err:  # noqa: BLE001 - the boundary with a third-party SDK, whose exceptions are its own business
                raise EmbeddingError(f"voyage: {type(err).__name__}: {err}") from err
            vectors.extend(list(map(float, vector)) for vector in result.embeddings)
        if len(vectors) != len(texts):
            raise EmbeddingError(f"voyage: {len(vectors)} vectors for {len(texts)} texts")
        return vectors
