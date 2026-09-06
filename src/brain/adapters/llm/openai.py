# The third candidate of section 8.4. Nothing outside this file imports the openai SDK.
# It is not in ADR-0018, which decides two providers: the ADR is short one line, and the code is what has three.

from typing import Any

from openai import AsyncOpenAI

from adapters.llm.base import (
    DEFAULT_MAX_TOKENS,
    SDK_RETRIES,
    Completion,
    LLMMessage,
    LLMRefusal,
    calling,
)

PROVIDER = "openai"

_REFUSED = "content_filter"


class OpenAIClient:
    provider = PROVIDER

    def __init__(self, api_key: str, timeout: float) -> None:
        # The SDK gets the timeout too, so the socket closes instead of leaking when asyncio cancels the call.
        self._client = AsyncOpenAI(api_key=api_key, timeout=timeout, max_retries=SDK_RETRIES)
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
            # max_completion_tokens and not max_tokens: the newer models reject the deprecated name outright.
            "max_completion_tokens": max_tokens or DEFAULT_MAX_TOKENS,
        }
        if temperature is not None:
            payload["temperature"] = temperature

        async with calling(PROVIDER, self._timeout):
            response = await self._client.chat.completions.create(**payload)

        choice = response.choices[0] if response.choices else None
        if choice is not None and choice.finish_reason == _REFUSED:
            raise LLMRefusal(f"{PROVIDER}: the model declined to answer")

        return Completion(
            text=(choice.message.content or "") if choice is not None else "",
            model=model,
            prompt_tokens=_tokens(response, "prompt_tokens"),
            completion_tokens=_tokens(response, "completion_tokens"),
        )


# Like Cohere, the system prompt travels as the first message.
def _messages(system: str | None, messages: tuple[LLMMessage, ...]) -> list[dict[str, str]]:
    prefix = [{"role": "system", "content": system}] if system else []
    return prefix + [{"role": m.role, "content": m.text} for m in messages]


def _tokens(response: Any, field: str) -> int:
    value = getattr(getattr(response, "usage", None), field, None)
    return int(value) if value is not None else 0
