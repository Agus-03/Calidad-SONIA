# Resolves ai_agents.llm_provider to a client, once per turn: the provider is a property of the row and not of the deployment,
# so the process cannot decide it at boot (regla 10, and the reason no key is required to start).

from collections.abc import Callable
from dataclasses import dataclass
from functools import lru_cache

from adapters.llm import anthropic, cohere, double, openai
from adapters.llm.base import Completion, LLMClient, LLMMessage, LLMUnavailable
from infra.config import Settings


@dataclass(frozen=True)
class _Provider:
    build: Callable[[str, float], LLMClient]
    # The Settings attribute holding this provider's key, so adding a provider is one row here and nothing else.
    credential: str


_PROVIDERS: dict[str, _Provider] = {
    cohere.PROVIDER: _Provider(cohere.CohereClient, "cohere_api_key"),
    anthropic.PROVIDER: _Provider(anthropic.AnthropicClient, "anthropic_api_key"),
    openai.PROVIDER: _Provider(openai.OpenAIClient, "openai_api_key"),
}


# Returned instead of raised so resolve() has one return type, and every failure of a turn arrives at one except (LLMError).
class UnavailableClient:
    def __init__(self, provider: str, reason: str) -> None:
        self.provider = provider
        self._reason = reason

    async def complete(self, **_: object) -> Completion:
        raise LLMUnavailable(f"{self.provider}: {self._reason}")


# Keyed on the three values that define a connection and not on Settings, which Pydantic does not make hashable.
@lru_cache(maxsize=8)
def _client(provider: str, api_key: str, timeout: float) -> LLMClient:
    return _PROVIDERS[provider].build(api_key, timeout)


def resolve(provider: str, settings: Settings) -> LLMClient:
    # Wins over the row on purpose: development and CI have no key, and the guard against production is in Settings.
    if settings.llm_use_double:
        return double.DoubleClient()

    entry = _PROVIDERS.get(provider)
    if entry is None:
        return UnavailableClient(provider, f"unknown provider, expected one of {_known()}")

    api_key = getattr(settings, entry.credential)
    if not api_key:
        return UnavailableClient(provider, f"{entry.credential.upper()} is not configured")

    return _client(provider, api_key, settings.llm_timeout_seconds)


def _known() -> str:
    return ", ".join(sorted(_PROVIDERS))


# The cache holds SDK clients with open connection pools, so a test that changes credentials has to be able to drop them.
def clear_clients() -> None:
    _client.cache_clear()


__all__ = ["Completion", "LLMClient", "LLMMessage", "clear_clients", "resolve"]
