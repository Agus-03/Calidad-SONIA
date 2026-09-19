# The generate step of ADR-0016: the cost is measured here and not estimated later, because generations is what OE2 reads.

import time
from dataclasses import dataclass
from decimal import ROUND_HALF_UP, Decimal
from functools import lru_cache
from pathlib import Path

import yaml

from adapters.llm.base import LLMClient, LLMMessage
from infra import ids
from infra.enums import GenerationStatus, MessageDirection
from pipeline.repository import Generation, Turn
from prompts.assembler import Prompt

# Anchored to the package and not to the working directory: the brain runs in a container, where neither is the repository.
_PRICING_FILE = Path(__file__).resolve().parent.parent / "rules" / "pricing.yaml"

# Providers publish per million tokens, and keeping the file in those units is what makes it transcribable by hand.
_PER_MILLION = Decimal(1_000_000)

# The scale of generations.cost, numeric(14,6), quantised here so the row, the log and the report carry the same number.
_SCALE = Decimal("0.000001")


# Raised while loading, never while answering: a tariff file that cannot be read is a deployment mistake.
class PricingError(RuntimeError):
    pass


# A configuration error and not a provider failure, so it stays out of the LLMError hierarchy that decides what to retry.
class UnpricedModel(RuntimeError):
    pass


@dataclass(frozen=True)
class Rate:
    input_per_million: Decimal
    output_per_million: Decimal
    currency: str


@dataclass(frozen=True)
class Pricing:
    # Not a column of generations: the tariff version reaches the log and nothing else, so no query can tell two tariffs apart.
    version: int
    currency: str
    rates: dict[tuple[str, str], Rate]


# Read once per process: the file is versioned data, so it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def pricing() -> Pricing:
    return _load(_PRICING_FILE)


def _load(path: Path) -> Pricing:
    try:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as err:
        raise PricingError(f"pricing: {path.name} cannot be read: {err}") from err

    if not isinstance(raw, dict):
        raise PricingError(f"pricing: {path.name} does not declare a mapping")

    version = raw.get("version")
    if not isinstance(version, int) or isinstance(version, bool):
        raise PricingError("pricing: version has to be an integer, which is what names a tariff in a measurement")

    currency = raw.get("currency")
    # char(3) in the schema, and one per file: mixing currencies would need a rate of exchange and a date (C08).
    if not isinstance(currency, str) or len(currency) != 3:
        raise PricingError("pricing: currency has to be the three letters generations.currency stores")

    providers = raw.get("providers")
    if not isinstance(providers, dict) or not providers:
        raise PricingError("pricing: providers has to be a mapping with at least one provider")

    rates: dict[tuple[str, str], Rate] = {}
    for provider, models in providers.items():
        if not isinstance(models, dict) or not models:
            raise PricingError(f"pricing: {provider} declares no model")
        for model, tariff in models.items():
            rates[(provider, model)] = _rate(provider, model, tariff, currency)

    return Pricing(version=version, currency=currency, rates=rates)


def _rate(provider: str, model: str, tariff: object, currency: str) -> Rate:
    if not isinstance(tariff, dict):
        raise PricingError(f"pricing: {provider}/{model} does not declare a mapping")

    prices: dict[str, Decimal] = {}
    for side in ("input", "output"):
        value = tariff.get(side)
        if not isinstance(value, (int, float)) or isinstance(value, bool) or value < 0:
            raise PricingError(f"pricing: {provider}/{model} needs a non-negative {side} price")
        # Through str(): a YAML float is already binary, so Decimal(2.50) is not 2.50 and the tariff would drift (C08).
        prices[side] = Decimal(str(value))

    return Rate(
        input_per_million=prices["input"],
        output_per_million=prices["output"],
        currency=currency,
    )


# Raises instead of falling back to a default tariff: a default writes a cost nobody can defend into the table OE2 reads.
def rate_for(provider: str, model: str) -> Rate:
    spec = pricing()
    rate = spec.rates.get((provider, model))
    if rate is None:
        known = ", ".join(f"{p}/{m}" for p, m in sorted(spec.rates))
        raise UnpricedModel(f"pricing: {provider}/{model} has no tariff, and these do: {known}")
    return rate


def cost_of(rate: Rate, prompt_tokens: int, completion_tokens: int) -> Decimal:
    spent = rate.input_per_million * prompt_tokens + rate.output_per_million * completion_tokens
    return (spent / _PER_MILLION).quantize(_SCALE, rounding=ROUND_HALF_UP)


# Takes no connection: the call takes seconds, and holding a pool connection through it starves the next turn.
async def generate(*, client: LLMClient, turn: Turn, prompt: Prompt) -> Generation:
    # Priced before the call and not after: failing afterwards spends the money and still writes no evidence of it.
    rate = rate_for(turn.agent.llm_provider, turn.agent.llm_model)

    # perf_counter and not the wall clock: latency_ms is a duration, and the wall clock can step backwards mid-turn.
    started = time.perf_counter()
    completion = await client.complete(
        model=turn.agent.llm_model,
        system=prompt.system,
        messages=_messages(turn, prompt.user),
        temperature=turn.agent.temperature,
        max_tokens=turn.agent.max_tokens,
    )
    latency_ms = round((time.perf_counter() - started) * 1000)

    return Generation(
        id=ids.new(),
        conversation_id=turn.conversation_id,
        ai_agent_id=turn.agent.id,
        message_batch_id=turn.message_batch_id,
        # The client that answered and not the row that chose it, so a doubled run says so in its own column.
        llm_provider=client.provider,
        llm_model=completion.model,
        prompt_rendered=_rendered(prompt),
        completion_text=completion.text,
        prompt_tokens=completion.prompt_tokens,
        completion_tokens=completion.completion_tokens,
        latency_ms=latency_ms,
        cost=cost_of(rate, completion.prompt_tokens, completion.completion_tokens),
        currency=rate.currency,
        # Written once and already with its verdict (07-secuencia 4.2): pending_review and blocked have no path here yet.
        status=GenerationStatus.AUTO_SENT,
        component_version_ids=tuple(c.version_id for c in prompt.assembled),
    )


# The history is left out on purpose: it is rows in messages, while the assembled envelope exists nowhere else.
def _rendered(prompt: Prompt) -> str:
    return f"{prompt.system}\n\n{prompt.user}"


# Real user/assistant turns and never one flattened string, which is the shape Regla 1 declares.
def _messages(turn: Turn, envelope: str) -> tuple[LLMMessage, ...]:
    # The operator's replies reach the model as the assistant's own: from the contact's side there is one voice answering.
    history = tuple(
        LLMMessage(
            role="user" if m.direction == MessageDirection.INBOUND else "assistant",
            text=m.content_text,
        )
        for m in turn.history
    )
    return (*history, LLMMessage(role="user", text=envelope))
