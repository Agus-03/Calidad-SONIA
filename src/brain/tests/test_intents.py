# The classifier of F11: how many different things the customer asked for, and what happens when the model does not cooperate.

from decimal import Decimal
from pathlib import Path

import pytest

from adapters.llm.base import Completion, LLMMessage, LLMUnavailable
from adapters.llm.double import INTENT_ANSWER, INTENT_MARKER, DoubleClient
from classifiers.intent import IntentRulesError, _load, classify, parse, rules


# The doubled adapter finds the classifier by a literal it holds itself, so nothing but this test stops the two from drifting
# apart. If they did, the doubled stack would run every path of the turn except the one F11 adds, and say nothing about it.
def test_the_doubled_adapter_and_the_prompt_agree_on_the_marker() -> None:
    assert rules().marker == INTENT_MARKER
    assert INTENT_MARKER in rules().system


# A prompt that stopped carrying its own marker is the same failure seen from the other side, and it has to stop the boot.
def test_a_prompt_without_its_marker_is_a_deployment_mistake(tmp_path: Path) -> None:
    broken = tmp_path / "intent.yaml"
    broken.write_text(
        'version: 1\nmarker: "[X]"\nmax_intents: 5\nsystem: "clasificá"\nuser: "[MENSAJE]"\n',
        encoding="utf-8",
    )

    with pytest.raises(IntentRulesError, match="marker"):
        _load(broken)


@pytest.mark.parametrize(
    ("name", "raw"),
    [
        ("no version names the prompt a measurement would cite", 'marker: "[X]"\nmax_intents: 5\nsystem: "[X]"\nuser: "u"\n'),
        ("max_intents below one would classify nothing, ever", 'version: 1\nmarker: "[X]"\nmax_intents: 0\nsystem: "[X]"\nuser: "u"\n'),
        ("no system prompt is a classifier with nothing to say", 'version: 1\nmarker: "[X]"\nmax_intents: 5\nuser: "u"\n'),
        ("a file that is not a mapping is not a prompt", "- uno\n- dos\n"),
    ],
)
def test_a_prompt_file_that_cannot_serve_stops_the_boot(tmp_path: Path, name: str, raw: str) -> None:
    broken = tmp_path / "intent.yaml"
    broken.write_text(raw, encoding="utf-8")

    with pytest.raises(IntentRulesError):
        _load(broken)


# The case F11 exists for: three things asked at once, kept in the order the customer said them.
def test_three_requests_become_three_intents_in_order() -> None:
    intents = parse(
        '[{"label": "consulta_disponibilidad", "confidence": 0.94},'
        ' {"label": "consulta_precio", "confidence": 0.71},'
        ' {"label": "quiere_visitar", "confidence": 0.5}]',
        max_intents=5,
    )

    assert [i.label for i in intents] == ["consulta_disponibilidad", "consulta_precio", "quiere_visitar"]
    assert [i.position for i in intents] == [1, 2, 3]
    assert intents[0].confidence == Decimal("0.94")


def test_one_request_becomes_one_intent() -> None:
    intents = parse('[{"label": "saludo", "confidence": 1}]', max_intents=5)

    assert len(intents) == 1
    assert intents[0].position == 1
    assert intents[0].confidence == Decimal("1.00")


# The customer's answer does not depend on this: losing the evidence of one turn is a smaller failure than not answering.
@pytest.mark.parametrize(
    ("name", "answer"),
    [
        ("the model wrote prose instead of JSON", "El cliente pregunta por disponibilidad y precio."),
        ("the model answered an object and not the list that was asked for", '{"label": "saludo"}'),
        ("the doubled generation prefix is not JSON either", "[doblado] hola"),
        ("an empty answer", "   "),
    ],
)
def test_an_answer_that_does_not_parse_leaves_no_intents_and_raises_nothing(name: str, answer: str) -> None:
    assert parse(answer, max_intents=5) == ()


# The most common way a model ignores "sin bloques de código", and three backticks are not worth losing a classification over.
def test_a_fenced_answer_is_unwrapped() -> None:
    intents = parse('```json\n[{"label": "consulta_precio", "confidence": 0.8}]\n```', max_intents=5)

    assert [i.label for i in intents] == ["consulta_precio"]


# One malformed element must not throw away the ones that came out fine, and the positions have to close the gap it left.
def test_a_malformed_element_is_dropped_and_the_rest_keeps_its_order() -> None:
    intents = parse(
        '[{"label": "consulta_precio", "confidence": 0.8},'
        ' {"confidence": 0.5},'
        ' "quiere_visitar",'
        ' {"label": "pide_fotos", "confidence": 0.6}]',
        max_intents=5,
    )

    assert [(i.label, i.position) for i in intents] == [("consulta_precio", 1), ("pide_fotos", 2)]


@pytest.mark.parametrize(
    ("name", "element"),
    [
        ("a confidence above one is not a confidence", '{"label": "x", "confidence": 1.4}'),
        ("a negative confidence is not one either", '{"label": "x", "confidence": -0.1}'),
        ("a confidence that is not a number", '{"label": "x", "confidence": "alta"}'),
        ("an empty label names nothing", '{"label": "   ", "confidence": 0.9}'),
        ("a label that is a sentence is the model writing prose into a column", f'{{"label": "{"x" * 61}", "confidence": 0.9}}'),
    ],
)
def test_an_element_that_is_not_an_intent_is_dropped(name: str, element: str) -> None:
    assert parse(f"[{element}]", max_intents=5) == ()


# Two ways of saying the same thing are one intention, which is what the prompt asks for and what the parser enforces.
def test_the_same_label_twice_is_one_intent() -> None:
    intents = parse(
        '[{"label": "consulta_precio", "confidence": 0.9}, {"label": "consulta_precio", "confidence": 0.4}]',
        max_intents=5,
    )

    assert len(intents) == 1
    assert intents[0].confidence == Decimal("0.90")


# A model that rambles must not write fifty rows of evidence for one message.
def test_more_intents_than_the_cap_are_cut() -> None:
    many = ", ".join(f'{{"label": "pedido_{n}", "confidence": 0.5}}' for n in range(10))

    intents = parse(f"[{many}]", max_intents=3)

    assert len(intents) == 3
    assert [i.position for i in intents] == [1, 2, 3]


# The whole classifier against the doubled adapter, which is what lets a stack with no key exercise F11 end to end.
async def test_the_doubled_adapter_classifies_the_batch() -> None:
    intents = await classify(client=DoubleClient(), model="command-a-03-2025", message="hola, esta disponible? cuanto sale?")

    assert [i.label for i in intents] == ["consulta_disponibilidad", "consulta_precio"]
    assert [i.position for i in intents] == [1, 2]


# The double answers the classifier and the generation differently, and what tells them apart is the system prompt.
async def test_the_double_answers_the_generation_as_before() -> None:
    completion = await DoubleClient().complete(
        model="m", system="sos un agente", messages=(LLMMessage(role="user", text="hola"),)
    )

    assert completion.text == "[doblado] hola"
    assert INTENT_ANSWER not in completion.text


# The classifier asks with temperature zero: this is evidence, and the same batch has to classify the same way twice.
async def test_the_classifier_asks_for_a_deterministic_answer() -> None:
    asked: dict[str, object] = {}

    class Recording:
        provider = "recording"

        async def complete(self, **kwargs: object) -> Completion:
            asked.update(kwargs)
            return Completion(text=INTENT_ANSWER, model="m", prompt_tokens=1, completion_tokens=1)

    await classify(client=Recording(), model="m", message="hola")

    assert asked["temperature"] == 0.0
    assert INTENT_MARKER in str(asked["system"])


# A provider that cannot answer is the turn's business and not this module's: it travels out as the LLMError it already is.
async def test_a_provider_that_cannot_answer_reaches_the_caller() -> None:
    class Down:
        provider = "down"

        async def complete(self, **_: object) -> Completion:
            raise LLMUnavailable("down: no key")

    with pytest.raises(LLMUnavailable):
        await classify(client=Down(), model="m", message="hola")
