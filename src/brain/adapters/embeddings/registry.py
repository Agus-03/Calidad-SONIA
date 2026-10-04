# Chooses the embeddings of the process: voyage-4, or the double where Settings allows it.

from functools import lru_cache

from adapters.embeddings import double, voyage
from adapters.embeddings.base import Embedder
from infra.config import Settings


# timeout is the turn's when the api searches (ADR-0074), and the worker's own when it is left out: a customer waits on one and
# nobody on the other.
def resolve(settings: Settings, *, timeout: float | None = None) -> Embedder:
    if settings.embeddings_use_double:
        return double.DoubleEmbedder()
    return _voyage(settings.voyage_api_key, timeout if timeout is not None else settings.voyage_timeout_seconds)


# One client per process: it holds an HTTP session.
@lru_cache(maxsize=2)
def _voyage(api_key: str, timeout: float) -> voyage.VoyageEmbedder:
    return voyage.VoyageEmbedder(api_key, timeout)
