# The backup provider of ADR-0018. Nothing outside this file imports the anthropic SDK.

from typing import Any

from anthropic import AsyncAnthropic

from adapters.llm.base import (
    DEFAULT_MAX_TOKENS,
    SDK_RETRIES,
    Completion,
    LLMMessage,
    LLMRefusal,
    calling,
)

PROVIDER = "anthropic"

_REFUSED = "refusal"


class AnthropicClient:
    provider = PROVIDER

    def __init__(self, api_key: str, timeout: float) -> None:
        # The SDK gets the timeout too, so the socket closes instead of leaking when asyncio cancels the call.
        self._client = AsyncAnthropic(api_key=api_key, timeout=timeout, max_retries=SDK_RETRIES)
        self._timeout = timeout

    async def complete(
        self,
        *,
        model: str,
        system: str | None,
        messages: tuple[LLMMessage, ...],
        temperature: float | None = None,
        max_tokens: int | None = None,
    ) -> Completion:
        payload: dict[str, Any] = {
            "model": model,
            # Required by this provider and nullable in ai_agents, which is why the port carries a default at all.
            "max_tokens": max_tokens or DEFAULT_MAX_TOKENS,
            "messages": [{"role": m.role, "content": m.text} for m in messages],
        }
        # A separate field and not the first message: this is the one shape difference between the three providers.
        if system:
            payload["system"] = system
        if temperature is not None:
            payload["temperature"] = temperature

        async with calling(PROVIDER, self._timeout):
            response = await self._client.messages.create(**payload)

        if getattr(response, "stop_reason", None) == _REFUSED:
            raise LLMRefusal(f"{PROVIDER}: the model declined to answer")

        return Completion(
            text=_text(response),
            model=model,
            prompt_tokens=_tokens(response, "input_tokens"),
            completion_tokens=_tokens(response, "output_tokens"),
        )


# Content is a list of blocks and only the text ones are the answer: a thinking block is not what gets sent to WhatsApp.
def _text(response: Any) -> str:
    blocks = getattr(response, "content", None) or []
    return "".join(block.text for block in blocks if getattr(block, "type", None) == "text")


def _tokens(response: Any, field: str) -> int:
    value = getattr(getattr(response, "usage", None), field, None)
    return int(value) if value is not None else 0
