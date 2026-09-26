# The extraction of F31 (ADR-0047): the sensitive three by the patterns of the checkpoint, the other three by a model over the
# redacted text, and a closed list of keys that nothing outside it can widen.

import json
from datetime import UTC, datetime
from decimal import Decimal
from pathlib import Path
from uuid import uuid4

import pytest
import yaml

from adapters.llm.base import Completion, LLMMessage, LLMRefusal
from adapters.llm.double import (
    EXTRACTION_ANSWER,
    EXTRACTION_MARKER,
    EXTRACTION_NAME_ANSWER,
    DoubleClient,
)
from classifiers import extraction
from infra import rules as rules_module
from infra.enums import MessageDirection, SenderType
from infra.rules import RulesFileError
from pipeline import guardrail
from pipeline.repository import Message


def said(text: str) -> Message:
    return Message(
        direction=MessageDirection.INBOUND,
        sender_type=SenderType.CONTACT,
        content_text=text,
        sent_at=datetime.now(UTC),
        id=uuid4(),
    )


class FakeClient:
    provider = "double"

    def __init__(self, answer: str) -> None:
        self.answer = answer
        self.system: str | None = None
        self.messages: tuple = ()

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        self.system = system
        self.messages = messages
        return Completion(text=self.answer, model=model, prompt_tokens=7, completion_tokens=3)


@pytest.fixture(autouse=True)
def _fresh_rules():
    extraction.rules.cache_clear()
    yield
    extraction.rules.cache_clear()


def test_the_shipped_prompt_carries_the_marker_the_double_answers_to() -> None:
    spec = extraction.rules()
    assert spec.marker == EXTRACTION_MARKER and EXTRACTION_MARKER in spec.system
    # The six of ADR-0047, section 2.2, split the way section 2.3 splits them.
    assert spec.sensitive == ("dni", "cuit_cuil", "cbu")
    assert spec.by_model == ("first_name", "last_name", "email")


def test_the_answers_of_the_double_are_ones_the_parser_reads() -> None:
    assert extraction.parse(EXTRACTION_ANSWER) == ({}, Decimal("0.90"))
    assert extraction.parse(EXTRACTION_NAME_ANSWER)[0] == {"first_name": "Lucía", "last_name": "Pérez"}


async def test_the_double_answers_the_extractor() -> None:
    completion = await DoubleClient().complete(
        model="m", system=extraction.rules().system, messages=(LLMMessage(role="user", text="hola, me llamo Lucía Pérez"),)
    )
    assert completion.text == EXTRACTION_NAME_ANSWER


def written(tmp_path: Path, monkeypatch: pytest.MonkeyPatch, **keys: list[str]) -> None:
    payload = {
        "version": 1,
        "marker": "[EXTRACTOR DE DATOS]",
        "keys": {"sensitive": ["dni", "cuit_cuil", "cbu"], "by_model": ["first_name", "last_name", "email"], **keys},
        "system": "[EXTRACTOR DE DATOS]\nsacá los datos.",
        "user": "[MENSAJE DEL CLIENTE]",
    }
    shipped = rules_module.RULES_DIR / "guardrails.yaml"
    (tmp_path / "guardrails.yaml").write_text(shipped.read_text(encoding="utf-8"), encoding="utf-8")
    (tmp_path / "extraction.yaml").write_text(yaml.safe_dump(payload, allow_unicode=True), encoding="utf-8")
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    guardrail.rules.cache_clear()
    extraction.rules.cache_clear()


# A sensitive key the guardrail has no pattern for would be listed and never found; a model key the parser does not read, asked
# for and dropped. Both are refused while loading, where they cost a deploy and not a silent gap.
@pytest.mark.parametrize(
    "keys",
    [{"sensitive": ["dni", "pasaporte"]}, {"by_model": ["first_name", "last_name", "email", "telefono"]}],
)
def test_a_key_nothing_would_fill_is_refused_while_loading(tmp_path, monkeypatch, keys) -> None:
    written(tmp_path, monkeypatch, **keys)
    try:
        with pytest.raises(RulesFileError):
            extraction.rules()
    finally:
        guardrail.rules.cache_clear()


# The checkpoint: the values the redaction replaces, in digits only, each from the message it was said in.
def test_the_checkpoint_takes_what_the_redaction_replaces_with_its_message() -> None:
    first = said("mi DNI es 30.123.456")
    second = said("y el CBU 0170099220000067797151, gracias")

    got = {e.key: e for e in extraction.sensitive((first, second))}

    assert got["dni"].value == "30123456" and got["dni"].source_message_id == first.id
    assert got["cbu"].value == "0170099220000067797151" and got["cbu"].source_message_id == second.id
    # A pattern is certain of what it matched.
    assert got["dni"].confidence == Decimal("1.00")


# The customer who writes two DNIs corrected the first one.
def test_the_last_value_of_a_key_in_the_batch_wins() -> None:
    got = extraction.sensitive((said("mi DNI es 30123456"), said("perdón, mi DNI es 30123457")))
    assert [e.value for e in got if e.key == "dni"] == ["30123457"]


def test_a_message_with_nothing_to_redact_extracts_nothing() -> None:
    assert extraction.sensitive((said("hola, ¿tienen departamentos?"),)) == ()


# The same text through both: what is extracted is exactly what the redaction took out, so nothing found reached the model.
def test_what_the_checkpoint_found_is_what_the_redaction_hid() -> None:
    text = "DNI 30.123.456 y CUIT 20-30123456-7"
    values = [value for _, value in guardrail.found(text)]
    redacted = guardrail._redact(text, guardrail.rules().redactions)[0]

    assert values and all(v not in redacted.replace(".", "").replace("-", "") for v in values)


async def test_the_model_reads_the_redacted_text_and_its_values_name_their_message() -> None:
    first, second = said("hola"), said("me llamo Lucía Pérez, lucia.perez@correo.com")
    client = FakeClient(json.dumps({"first_name": "Lucía", "last_name": "Pérez", "email": "Lucia.Perez@correo.com", "confidence": 0.8}))

    got = await extraction.classify(client=client, model="m", message="texto redactado", incoming=(first, second))

    assert client.messages[0].text.endswith("texto redactado")
    by_key = {e.key: e for e in got}
    assert by_key["first_name"].source_message_id == second.id
    assert by_key["email"].value == "lucia.perez@correo.com"
    assert by_key["last_name"].confidence == Decimal("0.80")


# A value the model wrote differently from the customer still needs an origin: the last message of the batch (ADR-0047, 2.7).
async def test_a_value_written_differently_falls_on_the_last_message() -> None:
    first, second = said("soy lucia"), said("¿tienen algo en Güemes?")
    client = FakeClient(json.dumps({"first_name": "Lucía", "last_name": None, "email": None, "confidence": 0.7}))

    got = await extraction.classify(client=client, model="m", message="x", incoming=(first, second))

    assert [(e.key, e.source_message_id) for e in got] == [("first_name", second.id)]


@pytest.mark.parametrize("answer", ["no es JSON", "[1]", '{"first_name": "Ana"}', '{"first_name": "Ana", "confidence": 2}'])
def test_an_answer_with_no_object_or_no_confidence_is_a_refusal(answer: str) -> None:
    with pytest.raises(LLMRefusal):
        extraction.parse(answer)


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("email", "no-es-un-mail"),
        ("first_name", "x" * 61),
        # What the redaction left is never a name: it would store "[DNI]" as a surname.
        ("last_name", "[DNI]"),
        ("first_name", 3),
        ("first_name", "   "),
    ],
)
def test_a_field_that_is_not_usable_is_dropped_and_the_rest_kept(field: str, value: object) -> None:
    payload = {"first_name": "Ana", "last_name": "Gómez", "email": None, "confidence": 0.9, field: value}
    values, _ = extraction.parse(json.dumps(payload))
    assert field not in values
    assert values.get("last_name" if field != "last_name" else "first_name")
