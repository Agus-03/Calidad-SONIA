# Chooses the embeddings of the process: voyage-4, or the double where Settings allows it.

from functools import lru_cache

from adapters.embeddings import double, voyage
from adapters.embeddings.base import Embedder
from infra.config import Settings


def resolve(settings: Settings) -> Embedder:
    if settings.embeddings_use_double:
        return double.DoubleEmbedder()
    return _voyage(settings.voyage_api_key, settings.voyage_timeout_seconds)


# One client per process: it holds an HTTP session.
@lru_cache(maxsize=2)
def _voyage(api_key: str, timeout: float) -> voyage.VoyageEmbedder:
    return voyage.VoyageEmbedder(api_key, timeout)
