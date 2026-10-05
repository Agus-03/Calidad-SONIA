# The embeddings of F06: the client of ADR-0067 lives here, in adapters/, as every provider's does (ADR-0017).

from collections.abc import Sequence
from typing import Literal, Protocol

# What Qdrant's collections are created with (ADR-0067, regla 2): changing it means recreating them (ADR-0064, regla 11).
DIMENSION = 1024

# Voyage embeds a document and a question differently, and asks to be told which one it is getting.
InputType = Literal["document", "query"]


# The one failure the worker needs to tell apart: the provider did not embed, and the document goes to failed with this reason.
class EmbeddingError(Exception):
    pass


class Embedder(Protocol):
    # Written in document_chunks.embedding_model, so a measurement of OE6 knows which model made each vector (ADR-0065, regla 9).
    model: str

    async def embed(self, texts: Sequence[str], *, input_type: InputType) -> list[list[float]]: ...
