# The port of one of the four declared boundaries (regla 11), and the only one OE2 turns into an instrument:
# comparing providers is a requirement, so the seam has to exist before there is anything to compare.

import asyncio
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from dataclasses import dataclass
from typing import Literal, Protocol

# The turn's retries belong to the core (ADR-0020): a second layer inside an SDK multiplies the wait the customer sees.
SDK_RETRIES = 0

# Anthropic demands max_tokens and ai_agents.max_tokens is nullable, so the port supplies the floor the schema does not.
DEFAULT_MAX_TOKENS = 1024


# Frozen because a port is data and not state: the same decision as the turn's dataclasses.
@dataclass(frozen=True)
class LLMMessage:
    # The two roles a conversation has here; the operator's messages reach the model as the assistant's own voice.
    role: Literal["user", "assistant"]
    text: str


# What generations needs and nothing more: the text to send, plus the two counts task 13 turns into cost.
@dataclass(frozen=True)
class Completion:
    text: str
    model: str
    prompt_tokens: int
    completion_tokens: int


# The hierarchy exists to decide, not to describe: only one of these three is worth asking again for.
class LLMError(Exception):
    pass


class LLMTimeout(LLMError):
    pass


# The provider answered and refused. Retrying spends the turn's budget on the same no.
class LLMRefusal(LLMError):
    pass


# No provider was reachable at all: an unknown name in ai_agents.llm_provider, or a credential nobody configured.
class LLMUnavailable(LLMError):
    pass


class LLMClient(Protocol):
    # Named after the row that chose it, so a log line says which adapter answered without inspecting the class.
    provider: str

    async def complete(
        self,
        *,
        model: str,
        system: str | None,
        messages: tuple[LLMMessage, ...],
        temperature: float | None = None,
        max_tokens: int | None = None,
    ) -> Completion: ...


# Wraps only the network call, never the extraction around it: a bug reading the response must not be reported as a provider failure.
@asynccontextmanager
async def calling(provider: str, timeout: float) -> AsyncIterator[None]:
    try:
        # asyncio and not each SDK's own timeout: three libraries raise three exceptions, and this one is the same for all.
        async with asyncio.timeout(timeout):
            yield
    except TimeoutError as err:
        raise LLMTimeout(f"{provider}: no answer in {timeout}s") from err
    except LLMError:
        raise
    except Exception as err:  # noqa: BLE001 - the boundary with a third-party SDK, whose exceptions are its own business
        raise LLMError(f"{provider}: {type(err).__name__}: {err}") from err
