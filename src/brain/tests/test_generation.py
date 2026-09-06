# The cost of a turn is evidence and not an estimate: OE2 compares providers with these numbers, and F35 reports them.

from datetime import UTC, datetime
from decimal import Decimal
from pathlib import Path
from uuid import uuid4

import pytest

from adapters.llm.base import Completion
from infra.enums import (
    GenerationStatus,
    MessageDirection,
    PromptComponentType,
    SenderType,
)
from pipeline.generation import (
    PricingError,
    Rate,
    UnpricedModel,
    _load,
    cost_of,
    generate,
    pricing,
    rate_for,
)
from pipeline.repository import Agent, Message, Turn
from prompts.assembler import assemble
from prompts.repository import Component

# What the demo agent runs on, and the one pair the shipped tariff has to price or the smoke test cannot generate.
PROVIDER = "cohere"
MODEL = "command-a-03-2025"

COHERE = Rate(input_per_million=Decimal("2.50"), output_per_million=Decimal("10.00"), currency="USD")


class FakeClient:
    # Announces itself as the double, so a test can prove the row names who answered and not who the row chose.
    provider = "double"

    def __init__(self, prompt_tokens: int = 1000, completion_tokens: int = 500) -> None:
        self.calls: list[str] = []
        self._prompt_tokens = prompt_tokens
        self._completion_tokens = completion_tokens

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        self.calls.append(model)
        return Completion(
            text="Sí, sigue disponible.",
            model=model,
            prompt_tokens=self._prompt_tokens,
            completion_tokens=self._completion_tokens,
        )


def turn(provider: str = PROVIDER, model: str = MODEL) -> Turn:
    message = Message(
        direction=MessageDirection.INBOUND,
        sender_type=SenderType.CONTACT,
        content_text="hola",
        sent_at=datetime.now(UTC),
    )
    return Turn(
        conversation_id=uuid4(),
        message_batch_id=uuid4(),
        turn_number=1,
        agent=Agent(
            id=uuid4(),
            name="Agente demo",
            llm_provider=provider,
            llm_model=model,
            temperature=0.3,
            max_tokens=512,
        ),
        incoming=(message,),
        history=(),
    )


def prompt():
    return assemble(
        agent_name="Agente demo",
        components=(Component(PromptComponentType.OBJECTIVE, "Ayudar a inquilinos.", 1),),
        message="hola",
    )


# The whole reason the price is looked up first: failing afterwards spends the money and still writes no evidence of it.
async def test_an_unpriced_model_fails_before_the_provider_is_called() -> None:
    client = FakeClient()

    with pytest.raises(UnpricedModel):
        await generate(client=client, turn=turn(model="un-modelo-que-nadie-tarifo"), prompt=prompt())

    assert client.calls == [], "the turn paid for a call it could not price"


# The message names what is priced, because the fix is editing rules/pricing.yaml and the operator has to know with what.
def test_an_unpriced_model_names_the_ones_that_are_priced() -> None:
    with pytest.raises(UnpricedModel) as raised:
        rate_for(PROVIDER, "command-inexistente")

    assert f"{PROVIDER}/{MODEL}" in str(raised.value)


# The arithmetic C08 exists for: 1000 in at 2.50 and 500 out at 10.00 per million is 0.0075, exactly and not nearly.
def test_the_cost_is_exact_at_the_scale_of_the_column() -> None:
    cost = cost_of(COHERE, prompt_tokens=1000, completion_tokens=500)

    assert cost == Decimal("0.007500")
    assert cost.as_tuple().exponent == -6, "the row is numeric(14,6) and the number written has to match it"


# The limit of the column said out loud: under a millionth of a dollar the row stores zero, which is precision and not drift.
def test_a_turn_cheaper_than_the_scale_is_stored_as_zero() -> None:
    assert cost_of(COHERE, prompt_tokens=0, completion_tokens=0) == Decimal("0.000000")


# Evidence names who answered: the row said cohere, the double answered, and a cost aggregate has to be able to say so.
async def test_the_generation_names_the_client_that_answered() -> None:
    generation = await generate(client=FakeClient(), turn=turn(), prompt=prompt())

    assert generation.llm_provider == "double"
    assert generation.llm_model == MODEL
    assert generation.cost == Decimal("0.007500")
    assert generation.currency == "USD"


async def test_the_generation_measures_the_call_it_made() -> None:
    generation = await generate(client=FakeClient(), turn=turn(), prompt=prompt())

    assert isinstance(generation.latency_ms, int)
    assert generation.latency_ms >= 0
    assert generation.prompt_tokens == 1000
    assert generation.completion_tokens == 500


# Written once and already with its verdict (07-secuencia section 4.2): there is no provisional value to correct later.
async def test_the_status_is_the_verdict_and_not_a_placeholder() -> None:
    generation = await generate(client=FakeClient(), turn=turn(), prompt=prompt())

    assert generation.status is GenerationStatus.AUTO_SENT


# prompt_rendered has to be readable as what the model read, or the evidence cannot be audited against the answer.
async def test_the_rendered_prompt_carries_the_scaffolding_and_the_envelope() -> None:
    generation = await generate(client=FakeClient(), turn=turn(), prompt=prompt())

    assert "Ayudar a inquilinos." in generation.prompt_rendered
    assert "[MENSAJE DEL CLIENTE]" in generation.prompt_rendered
    assert generation.completion_text == "Sí, sigue disponible."


# The pair the seeded agent runs on: without it in the file, the smoke test of the phase cannot generate anything.
def test_the_shipped_tariff_prices_the_model_the_demo_agent_runs_on() -> None:
    spec = pricing()

    assert isinstance(spec.version, int)
    assert spec.currency == "USD"
    assert rate_for(PROVIDER, MODEL).input_per_million > 0


_PRICED = "providers:\n  cohere:\n    command-a-03-2025:\n      input: 2.5\n      output: 10.0\n"


# A tariff file that cannot be read is a deployment mistake, and it has to stop the load instead of pricing a turn wrong.
@pytest.mark.parametrize(
    ("body", "reason"),
    [
        ("- esto es una lista\n", "a file that is not a mapping"),
        (f"currency: USD\n{_PRICED}", "no version"),
        (f"version: 1\n{_PRICED}", "no currency"),
        (f"version: 1\ncurrency: DOLAR\n{_PRICED}", "a currency that does not fit char(3)"),
        ("version: 1\ncurrency: USD\nproviders: {}\n", "no provider at all"),
        ("version: 1\ncurrency: USD\nproviders:\n  cohere: {}\n", "a provider with no model"),
        (
            "version: 1\ncurrency: USD\nproviders:\n  cohere:\n    command-a-03-2025: 2.5\n",
            "a tariff that is not a mapping",
        ),
        (
            "version: 1\ncurrency: USD\nproviders:\n  cohere:\n    command-a-03-2025:\n      input: 2.5\n",
            "no output price",
        ),
        (
            "version: 1\ncurrency: USD\nproviders:\n  cohere:\n    command-a-03-2025:\n"
            "      input: -1\n      output: 10.0\n",
            "a negative price",
        ),
    ],
)
def test_a_tariff_that_cannot_serve_fails_on_load(tmp_path: Path, body: str, reason: str) -> None:
    broken = tmp_path / "pricing.yaml"
    broken.write_text(body, encoding="utf-8")

    with pytest.raises(PricingError):
        _load(broken)


def test_a_tariff_that_is_not_there_fails_on_load(tmp_path: Path) -> None:
    with pytest.raises(PricingError):
        _load(tmp_path / "missing.yaml")


# Read through str() and never as a float: 2.5 as a binary float is not 2.5, and the tariff would drift by row (C08).
def test_the_tariff_is_read_as_decimal_and_not_as_a_float(tmp_path: Path) -> None:
    priced = tmp_path / "pricing.yaml"
    priced.write_text(f"version: 1\ncurrency: USD\n{_PRICED}", encoding="utf-8")

    rate = _load(priced).rates[(PROVIDER, MODEL)]

    assert rate.input_per_million == Decimal("2.5")
    assert cost_of(rate, prompt_tokens=3, completion_tokens=0) == Decimal("0.000008")
