# The doubled provider: it runs the whole turn without a key, and Settings is what keeps it out of production (regla 10).

from adapters.llm.base import Completion, LLMMessage, LLMUnavailable

PROVIDER = "double"

# Announces itself in the answer, because a canned reply that looks real is the failure this double could cause.
PREFIX = "[doblado]"

# The heading the classifier of F11 opens its system prompt with. Held as a literal and not imported, so an adapter does not
# have to know a pipeline step; rules/intents.yaml declares the same string and tests/test_intents.py fails if the two drift.
# Without this, "[doblado] hola" would reach json.loads and the doubled stack would run every path of the turn except F11's.
INTENT_MARKER = "[CLASIFICADOR DE INTENCIONES]"

# Two intentions, which is the case F11 exists for: one answer that covers everything the customer asked.
INTENT_ANSWER = '[{"label": "consulta_disponibilidad", "confidence": 0.9}, {"label": "consulta_precio", "confidence": 0.7}]'

# The heading the triage of ADR-0016 opens its system prompt with, held here for the same reason as the one above: without it
# "[doblado] hola" would reach the verdict reader, every doubled turn would end as failed, and the one step of the inbound leg
# that needs a provider would be the only one a stack with no key cannot exercise. rules/triage.yaml declares the same string.
TRIAGE_MARKER = "[TRIAGE]"

# In domain and not a greeting, which is the verdict that does not cut the turn: the doubled stack exists to walk all of it.
TRIAGE_ANSWER = '{"verdict": "in_domain", "confidence": 0.9, "greeting": false}'


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

        if system and TRIAGE_MARKER in system:
            return Completion(
                text=TRIAGE_ANSWER,
                model=model,
                prompt_tokens=sum(len(m.text.split()) for m in messages),
                completion_tokens=len(TRIAGE_ANSWER.split()),
            )

        if system and INTENT_MARKER in system:
            return Completion(
                text=INTENT_ANSWER,
                model=model,
                prompt_tokens=sum(len(m.text.split()) for m in messages),
                completion_tokens=len(INTENT_ANSWER.split()),
            )

        return Completion(
            text=f"{PREFIX} {last}",
            model=model,
            # Words and not tokens: task 13 needs two non-zero numbers to exercise the cost, not an accurate count.
            prompt_tokens=sum(len(m.text.split()) for m in messages),
            completion_tokens=len(last.split()) + 1,
        )
