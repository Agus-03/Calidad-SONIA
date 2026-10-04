# The primary provider of ADR-0018. Nothing outside this file imports the cohere SDK.

from typing import Any

from cohere import AsyncClientV2

from adapters.llm.base import (
    DEFAULT_MAX_TOKENS,
    SDK_RETRIES,
    Completion,
    LLMMessage,
    LLMRefusal,
    calling,
)

PROVIDER = "cohere"

# Cohere reports why it stopped; this is the value that means the model declined, not that it ran out of room.
_REFUSED = "ERROR_TOXIC"


class CohereClient:
    provider = PROVIDER

    def __init__(self, api_key: str, timeout: float) -> None:
        # The SDK gets the timeout too, so the socket closes instead of leaking when asyncio cancels the call.
        self._client = AsyncClientV2(api_key=api_key, timeout=timeout, max_retries=SDK_RETRIES)
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
            "messages": _messages(system, messages),
            "max_tokens": max_tokens or DEFAULT_MAX_TOKENS,
        }
        # Omitted and not sent as null: an absent temperature means the provider's default, and null is a value.
        if temperature is not None:
            payload["temperature"] = temperature

        async with calling(PROVIDER, self._timeout):
            response = await self._client.chat(**payload)

        if response.finish_reason == _REFUSED:
            raise LLMRefusal(f"{PROVIDER}: the model declined to answer")

        return Completion(
            text=_text(response),
            model=model,
            prompt_tokens=_tokens(response, "input_tokens"),
            completion_tokens=_tokens(response, "output_tokens"),
        )


# Cohere carries the system prompt as the first message and not as a separate field, unlike Anthropic.
def _messages(system: str | None, messages: tuple[LLMMessage, ...]) -> list[dict[str, Any]]:
    prefix: list[dict[str, Any]] = [{"role": "system", "content": system}] if system else []
    return prefix + [{"role": m.role, "content": _content(m)} for m in messages]


# The same parts as OpenAI's, which is what Cohere's vision models take; a message without images keeps the plain string.
def _content(message: LLMMessage) -> str | list[dict[str, Any]]:
    if not message.images:
        return message.text
    parts: list[dict[str, Any]] = [{"type": "image_url", "image_url": {"url": image.data_url()}} for image in message.images]
    return [*parts, {"type": "text", "text": message.text}]


# Content is a list of blocks and only the text ones are the answer: a thinking block is not what gets sent to WhatsApp.
def _text(response: Any) -> str:
    blocks = getattr(response.message, "content", None) or []
    return "".join(block.text for block in blocks if getattr(block, "type", None) == "text")


# Zero when absent, because a missing count must not stop a turn that already has its answer (task 13 records it as it came).
def _tokens(response: Any, field: str) -> int:
    tokens = getattr(getattr(response, "usage", None), "tokens", None)
    value = getattr(tokens, field, None)
    return int(value) if value is not None else 0
