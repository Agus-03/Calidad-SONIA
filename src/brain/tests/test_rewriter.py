# The rewriter of F04 (ADR-0048): what it asks the model, what it keeps of the answer, and what it refuses to write.
#
# What is asserted here and nowhere else: that an answer it cannot use is a refusal and never an empty context, because the
# route falls back to the context there was, and an empty one written instead would erase it (03-capa-prompts, Regla 3).

import json
from decimal import Decimal
from pathlib import Path
from types import MappingProxyType

import pytest
import yaml

from adapters.llm.base import Completion, LLMMessage, LLMRefusal
from adapters.llm.double import REWRITER_ANSWER, REWRITER_MARKER, DoubleClient
from classifiers.intent import Intent
from infra import rules as rules_module
from infra.rules import RulesFileError
from pipeline.repository import Context, content_of
from prompts import rewriter

PREVIOUS = Context(
    summary="Busca un departamento de 2 ambientes en Jesús María.",
    primary_intent="consulta_zona",
    objections=("el precio",),
    urgency_level=2,
    budget_signal=None,
    extracted_data=MappingProxyType({"zona": "Jesús María"}),
)

ANSWER = json.dumps(
    {
        "summary": "Busca un departamento de 2 ambientes en Jesús María, hasta $350.000.",
        "objections": ["el precio"],
        "urgency_level": 3,
        "budget_signal": "hasta 350000 ARS",
        "intent_changed": False,
    },
    ensure_ascii=False,
)


class FakeClient:
    provider = "double"

    def __init__(self, answer: str) -> None:
        self.answer = answer
        self.system: str | None = None
        self.messages: tuple = ()
        self.temperature: float | None = None

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        self.system = system
        self.messages = messages
        self.temperature = temperature
        return Completion(text=self.answer, model="modelo-que-respondio", prompt_tokens=7, completion_tokens=3)


# rules() caches for the whole process, so without this the first test to run would fix the file for all the rest.
@pytest.fixture(autouse=True)
def _fresh_rules():
    rewriter.rules.cache_clear()
    yield
    rewriter.rules.cache_clear()


def written(tmp_path: Path, monkeypatch: pytest.MonkeyPatch, **overrides: object) -> None:
    payload: dict = {
        "version": 1,
        "marker": "[REESCRITOR DE CONTEXTO]",
        "system": "[REESCRITOR DE CONTEXTO]\nmantené el contexto.",
        "previous": "[CONTEXTO ANTERIOR]",
        "user": "[MENSAJE DEL CLIENTE]",
        "max_objections": 2,
        "urgency_max": 3,
        "render": {
            "summary": "Resumen:",
            "primary_intent": "Intención principal:",
            "objections": "Objeciones:",
            "urgency_level": "Urgencia (1 a 3):",
            "budget_signal": "Presupuesto:",
        },
    }
    payload.update(overrides)
    (tmp_path / "rewriter.yaml").write_text(yaml.safe_dump(payload, allow_unicode=True), encoding="utf-8")
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    rewriter.rules.cache_clear()


def test_the_shipped_prompt_carries_the_marker_the_double_answers_to() -> None:
    spec = rewriter.rules()
    assert spec.marker == REWRITER_MARKER, "rules/rewriter.yaml and the doubled adapter stopped agreeing on the marker"
    assert REWRITER_MARKER in spec.system


# The double's answer has to be one the parser accepts, or the doubled stack falls back on every turn and the smoke sees no version.
def test_the_answer_of_the_double_is_a_context_the_parser_keeps() -> None:
    assert rewriter.parse(REWRITER_ANSWER, rewriter.rules()).summary.startswith("[doblado]")


async def test_the_double_answers_the_rewriter_and_not_with_an_echo() -> None:
    completion = await DoubleClient().complete(
        model="m", system=rewriter.rules().system, messages=(LLMMessage(role="user", text="hola"),)
    )
    assert completion.text == REWRITER_ANSWER


def test_a_prompt_that_lost_the_marker_is_refused_while_loading(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, system="mantené el contexto.")
    with pytest.raises(RulesFileError):
        rewriter.rules()


async def test_the_context_there_was_reaches_the_model_before_the_message() -> None:
    client = FakeClient(ANSWER)

    await rewriter.rewrite(client=client, model="m", previous=PREVIOUS, message="¿y hasta 350 mil?")

    asked = client.messages[0].text
    assert asked.index("[CONTEXTO ANTERIOR]") < asked.index("[MENSAJE DEL CLIENTE]")
    assert "Jesús María" in asked and asked.endswith("¿y hasta 350 mil?")
    # Zero, because OE4 compares intent_changed against annotations and the same batch has to be read the same way twice.
    assert client.temperature == 0.0


async def test_a_first_turn_is_rewritten_with_no_previous_section() -> None:
    client = FakeClient(ANSWER)

    await rewriter.rewrite(client=client, model="m", previous=None, message="hola")

    assert "[CONTEXTO ANTERIOR]" not in client.messages[0].text


# primary_intent is F11's and extracted_data is F31's: a rewrite that reset them would erase what another classifier wrote.
async def test_the_rewrite_keeps_what_is_not_the_rewriters_to_decide() -> None:
    result = await rewriter.rewrite(client=FakeClient(ANSWER), model="m", previous=PREVIOUS, message="hola")

    assert result.context.primary_intent == "consulta_zona"
    assert dict(result.context.extracted_data) == {"zona": "Jesús María"}
    assert result.context.budget_signal == "hasta 350000 ARS"
    # The model that answered and not the one asked for: rewritten_by_model is evidence of who wrote the context.
    assert result.model == "modelo-que-respondio"
    assert result.latency_ms >= 0


@pytest.mark.parametrize(
    "answer",
    ["no es JSON", "[1, 2]", '{"objections": []}', '{"summary": "   "}', '{"summary": 3}'],
)
def test_an_answer_that_is_not_a_context_is_a_refusal_and_never_an_empty_one(answer: str) -> None:
    with pytest.raises(LLMRefusal):
        rewriter.parse(answer, rewriter.rules())


def test_an_answer_wrapped_in_a_code_fence_is_still_read() -> None:
    assert rewriter.parse(f"```json\n{ANSWER}\n```", rewriter.rules()).urgency_level == 3


@pytest.mark.parametrize("urgency", [0, 4, "2", True, 2.5])
def test_an_urgency_outside_the_scale_is_not_judged(urgency: object) -> None:
    answer = json.dumps({"summary": "algo", "urgency_level": urgency})
    assert rewriter.parse(answer, rewriter.rules()).urgency_level is None


def test_the_objections_are_capped_deduplicated_and_only_text(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch)
    answer = json.dumps({"summary": "algo", "objections": ["el precio", 3, "el precio", " ", "la zona", "la demora"]})

    assert rewriter.parse(answer, rewriter.rules()).objections == ("el precio", "la zona")


def test_a_budget_that_is_a_paragraph_is_not_a_signal() -> None:
    answer = json.dumps({"summary": "algo", "budget_signal": "x" * 61})
    assert rewriter.parse(answer, rewriter.rules()).budget_signal is None


@pytest.mark.parametrize(("value", "expected"), [(True, True), (False, False), ("true", None), (None, None)])
def test_intent_changed_is_kept_only_when_the_model_judged_it(value: object, expected: bool | None) -> None:
    answer = json.dumps({"summary": "algo", "intent_changed": value})
    assert rewriter.parse(answer, rewriter.rules()).intent_changed is expected


def test_the_primary_intent_is_the_most_confident_and_the_first_said_on_a_tie() -> None:
    intents = (
        Intent(label="consulta_precio", confidence=Decimal("0.70"), position=1),
        Intent(label="pide_fotos", confidence=Decimal("0.90"), position=2),
        Intent(label="quiere_visitar", confidence=Decimal("0.90"), position=3),
    )
    assert rewriter.primary(intents) == "pide_fotos"
    assert rewriter.primary(()) is None


def test_the_rendered_context_leaves_out_what_is_empty() -> None:
    text = rewriter.render(PREVIOUS)

    assert text.splitlines() == [
        "Resumen: Busca un departamento de 2 ambientes en Jesús María.",
        "Intención principal: consulta_zona",
        "Objeciones: el precio",
        "Urgencia (1 a 3): 2",
    ]


# The core copies this object into conversation_contexts with SQL, so its keys are the columns of that table (ADR-0048).
def test_the_content_of_a_version_is_keyed_by_the_columns_the_core_copies() -> None:
    assert set(content_of(PREVIOUS)) == {
        "summary",
        "primary_intent",
        "objections_json",
        "urgency_level",
        "budget_signal",
        "extracted_data_json",
        "intent_changed",
    }
