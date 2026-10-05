# The two signals of F12 the triage judges in the same call: the request for a person and the last answer (ADR-0045, 2.1-2.2).

from decimal import Decimal

import pytest

from infra.enums import MessageDirection, PipelineStep, SenderType
from pipeline import triage
from pipeline.repository.evidence import TracedStep
from tests.triage.doubles import FakeClient, decide, said


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
