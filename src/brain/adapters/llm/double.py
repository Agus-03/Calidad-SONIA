# The doubled provider: it runs the whole turn without a key, and Settings is what keeps it out of production (regla 10).

from adapters.llm.base import Completion, LLMMessage, LLMUnavailable

PROVIDER = "double"

# Announces itself in the answer, because a canned reply that looks real is the failure this double could cause.
PREFIX = "[doblado]"


class DoubleClient:
    provider = PROVIDER

    async def complete(
        self,
        *,
        model: str,
        system: str | None,
        messages: tuple[LLMMessage, ...],
        temperature: float | None = None,
        max_tokens: int | None = None,
    ) -> Completion:
        # Echoes the last inbound message so the smoke test proves the turn carried it, and not just that something answered.
        last = next((m.text for m in reversed(messages) if m.role == "user"), "")
        if not last:
            raise LLMUnavailable(f"{PROVIDER}: the turn reached the model with nothing to answer")

        return Completion(
            text=f"{PREFIX} {last}",
            model=model,
            # Words and not tokens: task 13 needs two non-zero numbers to exercise the cost, not an accurate count.
            prompt_tokens=sum(len(m.text.split()) for m in messages),
            completion_tokens=len(last.split()) + 1,
        )
