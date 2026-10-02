# The guardrail of ADR-0016 on its inbound run: the net under the redaction checkpoint of 03-capa-prompts regla 4.
#
# Two things are asserted here that nowhere else can be. That a modified result does NOT halt — the runner obeys that field and
# never reads an outcome, so every step owes the check that its own two agree. And that no value of the four data of ADR-0006
# survives the step, neither in the text nor in the row it leaves behind.

from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

import pytest
import yaml

from infra import rules as rules_module
from infra.enums import MessageDirection, SenderType
from infra.rules import RulesFileError
from pipeline import guardrail
from pipeline.repository.turn import Agent, Message, Turn
from pipeline.step import StepRequest, StepResult

# Real formats, and they are the whole point of the file: a pattern is only as good as what it does to these.
CBU = "0170099220000067797151"
CUIT = "20-35412887-4"
DNI_DOTTED = "12.345.678"
DNI_BARE = "12345678"


class FakeClient:
    provider = "double"

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        raise AssertionError("the guardrail asked a provider to find a number a pattern already finds")


def turn() -> Turn:
    return Turn(
        conversation_id=uuid4(),
        message_batch_id=uuid4(),
        turn_number=1,
        agent=Agent(
            id=uuid4(),
            name="Agente demo",
            llm_provider="cohere",
            llm_model="command-a-03-2025",
            temperature=None,
            max_tokens=512,
        ),
        incoming=(
            Message(
                direction=MessageDirection.INBOUND,
                sender_type=SenderType.CONTACT,
                content_text="hola",
                sent_at=datetime.now(UTC),
            ),
        ),
        history=(),
    )


# rules() caches for the whole process, so without this the first case to run would fix the patterns for all the rest.
@pytest.fixture(autouse=True)
def _fresh_rules():
    guardrail.rules.cache_clear()
    yield
    guardrail.rules.cache_clear()


def written(tmp_path: Path, monkeypatch: pytest.MonkeyPatch, payload: dict) -> None:
    (tmp_path / "guardrails.yaml").write_text(yaml.safe_dump(payload, allow_unicode=True), encoding="utf-8")
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    guardrail.rules.cache_clear()


async def decide(text: str, direction: MessageDirection = MessageDirection.INBOUND) -> StepResult:
    return await guardrail.run(StepRequest(turn=turn(), client=FakeClient(), direction=direction, text=text))


# Against the file that ships, because the four data of ADR-0006 are not ours to invent and a test on a file written here would
# prove nothing about the one that runs in production.
def test_the_shipped_file_covers_the_four_data_of_adr_0006() -> None:
    spec = guardrail.rules()

    assert spec.version >= 1
    assert {rule.type for rule in spec.redactions} == {"cbu", "cuit_cuil", "dni"}
    # One marker for CUIT and CUIL together, which is the way out the note of ADR-0006 left open: reading eleven digits in a
    # message there is no way to tell one from the other, and naming one of the two would assert something nobody knows.
    assert {rule.marker for rule in spec.redactions} == {"[CBU]", "[CUIT/CUIL]", "[DNI]"}


async def test_a_message_with_nothing_to_redact_passes_untouched() -> None:
    result = await decide("hola, quiero alquilar el depto de Jesús María")

    assert result.outcome == "pass"
    assert result.halts is False
    assert result.text == "hola, quiero alquilar el depto de Jesús María"
    assert result.detail is None


# The example of ADR-0006 section 2, which is the one sentence the whole decision was written around.
async def test_the_example_of_adr_0006_comes_out_as_the_adr_writes_it() -> None:
    result = await decide(f"Te paso mi CBU {CBU} para la seña.")

    assert result.text == "Te paso mi CBU [CBU] para la seña."
    assert result.outcome == "modified"
    # Modified is not a cut: the value is gone, so the customer is still owed an answer and the provider still never sees it.
    assert result.halts is False, "a redaction ended the turn, and a datum that was replaced is no reason to answer nobody"
    assert result.detail == {"redacted": {"cbu": 1}}


@pytest.mark.parametrize(
    ("text", "marker"),
    [
        (f"mi CBU es {CBU}", "[CBU]"),
        (f"mi CUIT es {CUIT}", "[CUIT/CUIL]"),
        (f"mi CUIT es {CUIT.replace('-', '')}", "[CUIT/CUIL]"),
        (f"mi DNI {DNI_DOTTED}", "[DNI]"),
        (f"mi DNI {DNI_BARE}", "[DNI]"),
    ],
)
async def test_no_value_of_the_four_data_survives_the_step(text: str, marker: str) -> None:
    result = await decide(text)

    assert marker in result.text
    for value in (CBU, CUIT, CUIT.replace("-", ""), DNI_DOTTED, DNI_BARE):
        assert value not in result.text, f"{value} reached the far end of the step and would reach the provider"


# The order of the file is the decision: a CBU read by the shorter patterns would come out as three CUITs, and the trace would
# report three findings for one datum.
async def test_a_cbu_is_one_finding_and_not_three() -> None:
    result = await decide(f"te paso el cbu {CBU}")

    assert result.detail == {"redacted": {"cbu": 1}}
    assert result.text.count("[CBU]") == 1


# Where this file earns its keep: 8 bare digits is also what a property costs, and a rule that claimed all of them would hand the
# customer back an answer with a marker where the price was.
async def test_a_price_is_not_a_document_number() -> None:
    result = await decide("el depto de Nueva Córdoba sale 85000000 pesos")

    assert result.outcome == "pass"
    assert "85000000" in result.text, "the guardrail redacted the price the customer had just asked for"


# And the cost of the rule above, written down rather than discovered later: once the message names a document, every bare number
# of that length in it is treated as one.
async def test_a_message_that_names_a_document_gives_up_its_other_numbers_too() -> None:
    result = await decide("mi documento es 12345678 y el depto sale 85000000")

    assert result.detail == {"redacted": {"dni": 2}}
    assert "85000000" not in result.text


# Whole word, so the rule is not satisfied by a longer word that happens to contain it.
async def test_a_word_that_merely_contains_the_context_word_does_not_arm_the_rule() -> None:
    result = await decide("el edificio es de adnia 12345678")

    assert result.outcome == "pass"


async def test_two_different_data_in_one_message_are_counted_apart() -> None:
    result = await decide(f"soy CUIT {CUIT} y el CBU para la seña es {CBU}")

    assert result.detail == {"redacted": {"cuit_cuil": 1, "cbu": 1}}


# The same datum found by two rules is one type in the detail and not two: a DNI with dots and one without are the same datum.
async def test_the_two_ways_of_writing_a_dni_count_as_one_type() -> None:
    result = await decide(f"el titular es DNI {DNI_DOTTED} y el garante DNI {DNI_BARE}")

    assert result.detail == {"redacted": {"dni": 2}}


# The counts and nothing else: detail_json is evidence a person reads, and the value it counts is the one thing ADR-0006 exists to
# keep out of anything that gets written down.
async def test_the_detail_never_carries_the_value_it_counted() -> None:
    result = await decide(f"mi CBU {CBU}, mi CUIT {CUIT} y mi DNI {DNI_DOTTED}")

    written_detail = str(result.detail)
    for value in (CBU, CUIT, DNI_DOTTED):
        assert value not in written_detail, "the row that records a redaction carried the datum it redacted"


# varchar in the row, and for this step it is the file: a redaction has to name the version of the patterns that produced it.
async def test_the_version_of_the_file_is_what_the_row_will_carry(tmp_path, monkeypatch) -> None:
    written(
        tmp_path,
        monkeypatch,
        {
            "version": 4,
            "redactions": [{"type": "cbu", "marker": "[CBU]", "pattern": r"\d{22}", "requires_context": []}],
        },
    )

    assert (await decide("hola")).version == "4"
    assert (await decide(f"cbu {CBU}")).version == "4"


# Decision D3, and the reason is stronger than the edit rate of F02: the outbound text has already been through the external
# provider, so rewriting it would protect nothing ADR-0006 cares about and would only put a marker where the number was. So the
# answer is refused whole and comes back untouched, which is what the caller writes into generations.completion_text (4.2).
async def test_the_outbound_run_refuses_an_answer_and_never_rewrites_it() -> None:
    result = await decide(f"tu CBU es {CBU}", MessageDirection.OUTBOUND)

    assert result.outcome == "blocked"
    assert result.halts is True, "a refused answer did not end the turn and would have been sent"
    assert result.text == f"tu CBU es {CBU}", "the outbound run rewrote the answer instead of refusing it (D3)"
    assert result.detail == {"found": {"cbu": 1}}


async def test_an_answer_with_nothing_in_it_goes_out_untouched() -> None:
    result = await decide("el depto de Jesús María está disponible", MessageDirection.OUTBOUND)

    assert result.outcome == "pass"
    assert result.halts is False
    assert result.detail is None


# One way of finding for the two runs and opposite decisions about the finding, which is the whole of D3: the same sentence is
# repaired on the way in and refused on the way out.
async def test_the_same_datum_is_repaired_inbound_and_refused_outbound() -> None:
    text = f"te paso el CBU {CBU} para la seña"

    inbound = await decide(text, MessageDirection.INBOUND)
    outbound = await decide(text, MessageDirection.OUTBOUND)

    assert (inbound.outcome, inbound.halts) == ("modified", False)
    assert (outbound.outcome, outbound.halts) == ("blocked", True)
    assert "[CBU]" in inbound.text and CBU not in inbound.text
    assert CBU in outbound.text


# The cost of the rule above, and it is not hypothetical: this step cannot tell the agency own bank account from a customer one,
# so an administrator who writes it into a prompt component so the AIAgent can pass it on gets every such answer refused.
async def test_an_answer_that_repeats_the_agency_own_account_is_refused_too() -> None:
    result = await decide(f"para la seña transferí a nuestra cuenta, CBU {CBU}", MessageDirection.OUTBOUND)

    assert result.outcome == "blocked"


# ADR-0006 declares four data, so a file with no rule in it is not an empty policy the way an empty blocklist is: it is the
# mechanism of one of the six foundational problems of the anteproyecto, missing.
def test_a_file_with_no_redaction_is_refused(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, {"version": 1, "redactions": []})

    with pytest.raises(RulesFileError):
        guardrail.rules()


def test_a_pattern_that_does_not_compile_is_a_deployment_mistake(tmp_path, monkeypatch) -> None:
    written(
        tmp_path,
        monkeypatch,
        {"version": 1, "redactions": [{"type": "cbu", "marker": "[CBU]", "pattern": "(sin cerrar", "requires_context": []}]},
    )

    with pytest.raises(RulesFileError) as raised:
        guardrail.rules()

    # Named after its position, because one wrong rule out of twelve has to say which one it is.
    assert "redactions[1]" in str(raised.value)


# A marker with a digit in it would be read by the rules that run after it, so a CBU already replaced could come back out of the
# step counted a second time as something else.
def test_a_marker_that_holds_a_digit_is_refused(tmp_path, monkeypatch) -> None:
    written(
        tmp_path,
        monkeypatch,
        {"version": 1, "redactions": [{"type": "cbu", "marker": "[CBU-22]", "pattern": r"\d{22}", "requires_context": []}]},
    )

    with pytest.raises(RulesFileError):
        guardrail.rules()


# A context word travels into a pattern of its own, so it can be as wrong as the pattern can and has to fail the same way.
def test_a_context_word_that_does_not_compile_is_a_deployment_mistake(tmp_path, monkeypatch) -> None:
    written(
        tmp_path,
        monkeypatch,
        {
            "version": 1,
            "redactions": [
                {"type": "dni", "marker": "[DNI]", "pattern": r"\d{8}", "requires_context": ["(sin cerrar"]}
            ],
        },
    )

    with pytest.raises(RulesFileError):
        guardrail.rules()


# Written on every rule even when it is empty, the same way a route writes its roles: a pattern that needs a word nearby has to
# say so, and one that holds on its own has to say that too.
def test_a_rule_that_does_not_say_whether_it_needs_context_is_refused(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, {"version": 1, "redactions": [{"type": "cbu", "marker": "[CBU]", "pattern": r"\d{22}"}]})

    with pytest.raises(RulesFileError):
        guardrail.rules()
