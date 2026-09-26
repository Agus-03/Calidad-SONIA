# The triage of ADR-0016: the third step of the inbound leg, the first one of the turn that costs a provider call, and the one
# whose verdict OE6 reads as its denominator.
#
# What is asserted here and nowhere else: that the two labels of its outcome domain are the only two it can write, and that a
# verdict nobody could read loses the turn instead of quietly becoming one of them. A step that answered in_domain because the
# model rambled would write a row asserting a check that never happened, which is the thing pipeline_steps exists to prevent.

from datetime import UTC, datetime
from decimal import Decimal
from pathlib import Path
from uuid import uuid4

import pytest
import yaml

from adapters.llm.base import Completion, LLMError, LLMRefusal
from adapters.llm.double import TRIAGE_MARKER
from infra import rules as rules_module
from infra.enums import MessageDirection, PipelineStep, PromptComponentType, SenderType
from infra.rules import RulesFileError
from pipeline import triage
from pipeline.repository import Agent, Message, TracedStep, Turn
from pipeline.step import StepRequest, StepResult
from prompts.repository import Component

IN_DOMAIN = '{"verdict": "in_domain", "confidence": 0.93, "greeting": false}'
OUT_OF_DOMAIN = '{"verdict": "out_of_domain", "confidence": 0.88, "greeting": false}'
GREETING = '{"verdict": "in_domain", "confidence": 0.99, "greeting": true}'

OBJECTIVE = "Alquilar y vender inmuebles en la ciudad de Córdoba."


# Records what it was asked, because half of what this step does is build the question: the objective has to reach the model or
# the verdict is about nothing.
class FakeClient:
    provider = "double"

    def __init__(self, answer: str) -> None:
        self.answer = answer
        self.calls = 0
        self.system: str | None = None
        self.messages: tuple = ()
        self.temperature: float | None = None

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        self.calls += 1
        self.system = system
        self.messages = messages
        self.temperature = temperature
        return Completion(text=self.answer, model=model, prompt_tokens=7, completion_tokens=3)


def turn(history: tuple[Message, ...] = ()) -> Turn:
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
        history=history,
    )


def said(text: str, direction: MessageDirection, sender: SenderType) -> Message:
    return Message(direction=direction, sender_type=sender, content_text=text, sent_at=datetime.now(UTC))


# Two of the six of F01, and the objective deliberately not first: the administrator orders them (Regla 1), so this step has to
# find it by type and never by position.
def components(objective: str | None = OBJECTIVE) -> tuple[Component, ...]:
    out = [Component(type=PromptComponentType.RULES, content="No tomar pagos por este canal.", position=1)]
    if objective is not None:
        out.append(Component(type=PromptComponentType.OBJECTIVE, content=objective, position=2))
    return tuple(out)


# rules() caches for the whole process, which is what production wants and what would otherwise let the order of these tests
# decide their result: the first one to run would fix the prompt for all the rest.
@pytest.fixture(autouse=True)
def _fresh_rules():
    triage.rules.cache_clear()
    yield
    triage.rules.cache_clear()


# Writes the file the step actually reads, so what is under test is the shape of triage.yaml and not a dict built in Python.
def written(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    *,
    version: int = 1,
    marker: str = "[TRIAGE]",
    system: str | None = None,
) -> None:
    payload = {
        "version": version,
        "marker": marker,
        "system": system if system is not None else f"{marker}\ndecidí si el pedido es de este negocio.",
        "objective": "[A QUÉ SE DEDICA EL NEGOCIO]",
        "previous": "[ÚLTIMA RESPUESTA DEL ASISTENTE]",
        "user": "[MENSAJE DEL CLIENTE]",
        "thresholds": {"out_of_domain": 1, "explicit_request": 1, "no_resolution": 3},
    }
    (tmp_path / "triage.yaml").write_text(yaml.safe_dump(payload, allow_unicode=True), encoding="utf-8")
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    triage.rules.cache_clear()


async def decide(
    answer: str,
    *,
    text: str = "¿tienen departamentos de dos dormitorios en Nueva Córdoba?",
    comps: tuple[Component, ...] | None = None,
    client: FakeClient | None = None,
    history: tuple[Message, ...] = (),
) -> StepResult:
    return await triage.run(
        StepRequest(
            turn=turn(history),
            client=client or FakeClient(answer),
            direction=MessageDirection.INBOUND,
            text=text,
            components=components() if comps is None else comps,
        )
    )


# The drift guard between a prompt and an adapter that has to recognise it: the doubled stack finds this step by the literal, so
# a prompt that stopped carrying it would leave every doubled turn failing with nothing saying why.
def test_the_shipped_prompt_carries_the_marker_the_double_answers_to() -> None:
    spec = triage.rules()

    assert spec.marker == TRIAGE_MARKER, "rules/triage.yaml and the doubled adapter stopped agreeing on the marker"
    assert spec.marker in spec.system


# The prompt may not invent a third label, because the outcome domain of this step is declared by DER/entidades.md and a value
# outside it is a row no query can read.
def test_the_shipped_prompt_names_the_two_labels_the_row_may_carry() -> None:
    system = triage.rules().system

    assert "in_domain" in system
    assert "out_of_domain" in system


async def test_a_question_about_the_business_lets_the_turn_go_on() -> None:
    result = await decide(IN_DOMAIN)

    assert result.outcome == "in_domain"
    assert result.halts is False
    assert result.score == Decimal("0.93")


# The cut of this step, and the decision the group took in O1c: out of the domain the turn ends, nothing automatic is sent, and
# the conversation goes to a person. Same shape as the cut of 07-secuencia 4.1.
async def test_a_question_that_is_not_the_business_ends_the_turn() -> None:
    result = await decide(OUT_OF_DOMAIN, text="hola, dame la receta de un budín")

    assert result.outcome == "out_of_domain"
    assert result.halts is True, "a turn outside the domain kept walking and was about to be answered by the AIAgent"
    assert result.text == "hola, dame la receta de un budín", "a step that only decides rewrote a text"


# Decision O1b: a message that is only a greeting is inside the domain, because attending whoever writes is part of what the
# business does. The flag is evidence and not a verdict — it is what lets the measurement of OE6 leave the greetings out of its
# denominator without a third value in an outcome domain that C07 does not let depend on another column.
async def test_a_greeting_is_in_domain_and_is_marked_so_oe6_can_leave_it_out() -> None:
    result = await decide(GREETING, text="hola, buenas tardes")

    assert result.outcome == "in_domain"
    assert result.halts is False
    assert result.detail["greeting"] is True, "a greeting left no mark, so OE6 counts it as a query of the domain resolved"


async def test_a_question_is_not_marked_as_a_greeting() -> None:
    assert (await decide(IN_DOMAIN)).detail["greeting"] is False


# Decision D1: what the domain of an agent is belongs to whoever configured it, so it travels as the objective component of F01
# and never as a list of topics in this repository.
async def test_the_objective_of_the_agent_is_what_the_model_compares_against() -> None:
    client = FakeClient(IN_DOMAIN)

    await decide(IN_DOMAIN, client=client)

    assert OBJECTIVE in client.system, "the triage asked a model to judge a domain without telling it what the business is"
    assert triage.rules().marker in client.system


async def test_the_customer_message_is_what_the_model_is_asked_about() -> None:
    client = FakeClient(IN_DOMAIN)

    await decide(IN_DOMAIN, text="¿cuánto sale el depto de Alberdi?", client=client)

    assert "¿cuánto sale el depto de Alberdi?" in client.messages[0].text
    # Zero so the same message triages the same way twice: this verdict is what a measurement rests on, not prose.
    assert client.temperature == 0.0


# The declared cost of D1, and the reason it is a cost and not a bug: an agent whose administrator never wrote what the business
# does has nothing to compare against, and refusing everybody would leave that whole agent unable to answer anyone.
async def test_an_agent_with_no_objective_answers_instead_of_refusing_everybody() -> None:
    client = FakeClient(OUT_OF_DOMAIN)

    result = await decide(OUT_OF_DOMAIN, comps=components(objective=None), client=client)

    assert result.outcome == "in_domain"
    assert result.halts is False
    assert result.detail == {"objective": "missing"}, "nothing in the row says why this turn was never actually triaged"
    assert client.calls == 0, "the triage paid a provider to compare a message against nothing"


async def test_an_objective_that_is_only_whitespace_counts_as_missing() -> None:
    result = await decide(IN_DOMAIN, comps=components(objective="   "))

    assert result.detail == {"objective": "missing"}


# The rule this whole module turns on: the domain of this step has no value for "could not decide", so a verdict nobody could
# read has to lose the turn. Writing in_domain instead would be a row claiming a check that never ran.
async def test_an_answer_that_is_not_json_loses_the_turn_instead_of_inventing_a_verdict() -> None:
    with pytest.raises(LLMRefusal):
        await decide("me parece que sí, está dentro del dominio")


async def test_a_verdict_outside_the_two_labels_is_refused() -> None:
    with pytest.raises(LLMRefusal):
        await decide('{"verdict": "quizás", "confidence": 0.9, "greeting": false}')


async def test_an_answer_that_is_json_but_not_an_object_is_refused() -> None:
    with pytest.raises(LLMRefusal):
        await decide('["in_domain"]')


@pytest.mark.parametrize("confidence", ["1.7", "-0.2"])
async def test_a_confidence_outside_zero_and_one_is_refused(confidence: str) -> None:
    with pytest.raises(LLMRefusal):
        await decide('{"verdict": "in_domain", "confidence": %s, "greeting": false}' % confidence)


async def test_a_confidence_that_is_not_a_number_is_refused() -> None:
    with pytest.raises(LLMRefusal):
        await decide('{"verdict": "in_domain", "confidence": "mucha", "greeting": false}')


# What the step raises has to be something the runner catches, or a bad answer becomes a 500 instead of a failed turn with its
# partial evidence kept (07-secuencia section 5).
def test_what_a_bad_verdict_raises_is_what_the_runner_is_watching_for() -> None:
    assert issubclass(LLMRefusal, LLMError)


# Three backticks are the commonest way a model ignores "sin bloques de código", and losing a turn over them is not worth it.
async def test_an_answer_wrapped_in_a_code_fence_is_still_read() -> None:
    result = await decide(f"```json\n{IN_DOMAIN}\n```")

    assert result.outcome == "in_domain"


# A missing flag costs a measurement some precision; a lost turn costs a customer their answer. The verdict is what decides.
async def test_a_missing_greeting_flag_does_not_lose_the_turn() -> None:
    result = await decide('{"verdict": "in_domain", "confidence": 0.5}')

    assert result.outcome == "in_domain"
    assert result.detail["greeting"] is False


# Decimal and not float, because pipeline_steps.score is numeric: a confidence that arrives as binary is a number nobody can
# compare twice.
async def test_the_confidence_is_decimal_where_the_column_is_numeric() -> None:
    result = await decide('{"verdict": "in_domain", "confidence": 0.9333, "greeting": false}')

    assert isinstance(result.score, Decimal)
    assert result.score == Decimal("0.93")


# The model is evidence and the file is the identifier: version names what decided (regla 9), and for this step what decided is
# the prompt. Which model read it is recoverable from the row and not confusable with a version of ours.
async def test_the_model_that_read_the_prompt_travels_in_the_detail() -> None:
    result = await decide(IN_DOMAIN)

    assert result.detail["model"] == "command-a-03-2025"


async def test_the_version_of_the_file_is_what_the_row_will_carry(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, version=4)

    assert (await decide(IN_DOMAIN)).version == "4"


# Refused while loading and never while answering: a prompt that lost the marker would leave the doubled stack answering this
# step with prose, and the failure would show up as every turn failing rather than as the deployment mistake it is.
def test_a_prompt_that_lost_the_marker_is_refused_while_loading(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, system="decidí si el pedido es de este negocio.")

    with pytest.raises(RulesFileError):
        triage.rules()


def test_a_file_with_no_objective_heading_is_refused(tmp_path, monkeypatch) -> None:
    (tmp_path / "triage.yaml").write_text(
        'version: 1\nmarker: "[TRIAGE]"\nsystem: "[TRIAGE] decidí"\nuser: "[MENSAJE]"\n', encoding="utf-8"
    )
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    triage.rules.cache_clear()

    with pytest.raises(RulesFileError):
        triage.rules()


# ADR-0045, section 2.1: the same call judges the two signals of F12 that are read off the customer's words and the last answer.
async def test_a_request_for_a_person_travels_in_the_detail() -> None:
    answer = '{"verdict": "in_domain", "confidence": 0.9, "greeting": false, "explicit_request": true, "resolved": null}'

    result = await decide(answer, text="pasame con alguien del negocio")

    assert result.detail["explicit_request"] is True
    assert result.halts is False, "the step cut by itself, and whether a signal cuts depends on the policy the route reads"


# Section 2.2: with no answer of the AIAgent to judge there is no judgement, whatever the model said about one.
async def test_with_no_answer_of_the_agent_resolved_is_not_judged() -> None:
    answer = '{"verdict": "in_domain", "confidence": 0.9, "greeting": false, "explicit_request": false, "resolved": false}'
    client = FakeClient(answer)

    result = await decide(answer, client=client)

    assert result.detail["resolved"] is None, "a no_resolution was judged against an answer nobody showed the model"
    assert triage.rules().previous_heading not in client.messages[0].text


async def test_the_last_answer_of_the_agent_is_what_the_model_judges() -> None:
    answer = '{"verdict": "in_domain", "confidence": 0.9, "greeting": false, "explicit_request": false, "resolved": false}'
    client = FakeClient(answer)
    history = (
        said("¿tienen cocheras?", MessageDirection.INBOUND, SenderType.CONTACT),
        said("Sí, tenemos dos disponibles.", MessageDirection.OUTBOUND, SenderType.AI_AGENT),
    )

    result = await decide(answer, client=client, history=history)

    assert result.detail["resolved"] is False
    sent = client.messages[0].text
    assert "Sí, tenemos dos disponibles." in sent
    assert sent.index(triage.rules().previous_heading) < sent.index(triage.rules().user_heading)


# An answer an Operator or the system wrote is not the AIAgent's to be judged for (ADR-0045, section 2.2).
@pytest.mark.parametrize("sender", [SenderType.OPERATOR, SenderType.SYSTEM])
def test_an_answer_the_agent_did_not_write_is_not_judged(sender: SenderType) -> None:
    history = (
        said("Sí, tenemos dos disponibles.", MessageDirection.OUTBOUND, SenderType.AI_AGENT),
        said("Te escribo yo.", MessageDirection.OUTBOUND, sender),
        said("gracias", MessageDirection.INBOUND, SenderType.CONTACT),
    )

    assert triage.last_answer(history) is None


# Not a bool is not judged: a missing flag written as false would put a no in the table OE3 reads.
@pytest.mark.parametrize("value", ['"sí"', "1", "null"])
def test_a_signal_that_is_not_a_bool_is_not_judged(value: str) -> None:
    verdict = triage.parse(
        '{"verdict": "in_domain", "confidence": 0.9, "explicit_request": %s, "resolved": %s}' % (value, value)
    )

    assert verdict.explicit_request is None
    assert verdict.resolved is None


def test_the_verdict_is_read_back_from_the_row_it_left() -> None:
    row = TracedStep(
        step=PipelineStep.TRIAGE,
        direction=MessageDirection.INBOUND,
        position=3,
        outcome="out_of_domain",
        version="2",
        latency_ms=5,
        score=Decimal("0.8"),
        detail={"greeting": False, "explicit_request": True, "resolved": None},
    )

    verdict = triage.verdict_of(row)

    assert verdict is not None
    assert (verdict.label, verdict.explicit_request, verdict.resolved) == ("out_of_domain", True, None)
    missing = TracedStep(**{**row.__dict__, "outcome": "in_domain", "detail": {"objective": "missing"}})
    assert triage.verdict_of(missing) is None, "a triage that never called a provider was read as a judgement"


def test_the_shipped_defaults_fix_the_yes_no_signals_at_one() -> None:
    thresholds = triage.rules().thresholds

    assert thresholds[triage.JUDGED[0]] == 1
    assert thresholds[triage.JUDGED[1]] == 1
