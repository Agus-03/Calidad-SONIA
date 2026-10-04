# The doubled embeddings: the worker and the search run without a key, and Settings keeps them out of production (regla 10).

import hashlib
import math
import re
from collections.abc import Sequence

from adapters.embeddings.base import DIMENSION, InputType

# Named so a chunk embedded by the double says so in document_chunks.embedding_model, and no measurement mistakes it for voyage-4.
MODEL = "double"

_WORDS = re.compile(r"\w+|[^\w\s]")


class DoubleEmbedder:
    model = MODEL

    # A bag of words hashed into the dimension and normalised: deterministic, and two texts that share words land close, so a
    # doubled search still finds the chunk that says what was asked.
    async def embed(self, texts: Sequence[str], *, input_type: InputType) -> list[list[float]]:
        return [_vector(text) for text in texts]


def _vector(text: str) -> list[float]:
    vector = [0.0] * DIMENSION
    for word in _WORDS.findall(text.casefold()):
        digest = hashlib.blake2b(word.encode("utf-8"), digest_size=8).digest()
        vector[int.from_bytes(digest, "big") % DIMENSION] += 1.0
    norm = math.sqrt(sum(v * v for v in vector))
    if norm == 0:
        # Qdrant refuses a zero vector under cosine distance, and an empty text still has to be a point.
        vector[0] = 1.0
        return vector
    return [v / norm for v in vector]
