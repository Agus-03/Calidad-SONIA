# The contract of a step of ADR-0016, written before the steps: what a row of pipeline_steps needs, what only the runner knows,
# and how a step ends a turn without the runner having to know the outcome of all seven.

import dataclasses
from datetime import UTC, datetime
from decimal import Decimal
from uuid import uuid4

import pytest

from adapters.llm.base import Completion
from infra.enums import MessageDirection, PipelineStep, SenderType
from pipeline.repository.turn import Agent, Message, Turn
from pipeline.step import Step, StepRequest, StepResult


class FakeClient:
    provider = "double"

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        return Completion(text="listo", model=model, prompt_tokens=1, completion_tokens=1)


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
            temperature=0.3,
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


def request(text: str = "hola", direction: MessageDirection = MessageDirection.INBOUND) -> StepRequest:
    return StepRequest(turn=turn(), client=FakeClient(), direction=direction, text=text)


# A step that changes nothing is the common case, and it has to be the cheap one to write.
async def passing(req: StepRequest) -> StepResult:
    return StepResult(outcome="pass", version="1", text=req.text)


# The two things a cut does: it says the turn ends here, and it leaves a row saying why.
async def blocking(req: StepRequest) -> StepResult:
    return StepResult(outcome="blocked", version="1", text=req.text, halts=True, detail={"term": "prohibido"})


# Neither position nor latency_ms is a field: the order of the turn and the clock around the call belong to the runner, and a
# step that reported either would be able to disagree with the trace it is part of.
def test_a_result_carries_no_field_the_runner_owns() -> None:
    fields = {f.name for f in dataclasses.fields(StepResult)}

    assert "position" not in fields
    assert "latency_ms" not in fields


# Frozen for the same reason as the ports: a result is evidence on its way to a row, and evidence nobody may edit afterwards.
def test_a_result_cannot_be_edited_after_the_step_decided() -> None:
    result = StepResult(outcome="pass", version="1", text="hola")

    with pytest.raises(dataclasses.FrozenInstanceError):
        result.outcome = "blocked"


def test_a_request_cannot_be_edited_by_the_step_that_reads_it() -> None:
    req = request()

    with pytest.raises(dataclasses.FrozenInstanceError):
        req.text = "otra cosa"


# The defaults are what most steps leave alone: the turn goes on, and only triage and grounding produce a score.
def test_the_defaults_are_a_step_that_decided_nothing_special() -> None:
    result = StepResult(outcome="pass", version="1", text="hola")

    assert result.halts is False
    assert result.score is None
    assert result.detail is None


# The text is always present, so no reader has to know that an absent one meant "unchanged": the redaction of ADR-0006 is a
# step whose whole effect is this field, and a step that passes has to echo what it was handed.
async def test_the_text_that_continues_comes_out_of_the_step() -> None:
    result = await passing(request(text="te paso mi CBU"))

    assert result.text == "te paso mi CBU"


# What halts exists for: the runner obeys the field, so the domain of outcome —declared per step by ADR-0016— stays out of it.
async def test_a_step_ends_the_turn_by_saying_so_and_not_by_its_outcome() -> None:
    result = await blocking(request())

    assert result.halts is True
    assert result.detail == {"term": "prohibido"}


# score is Decimal and not float because the column is numeric: a confidence that arrives as binary is a number nobody can
# compare twice, which is the same rule the intent classifier already follows.
def test_a_score_is_decimal_where_the_column_is_numeric() -> None:
    result = StepResult(outcome="in_domain", version="1", text="hola", score=Decimal("0.94"))

    assert result.score == Decimal("0.94")


# One entry is one row's two identifying columns plus what to call, so the order of the turn is data the runner walks.
async def test_an_entry_names_the_step_and_the_direction_of_its_row() -> None:
    entry = Step(step=PipelineStep.BLOCKLIST, direction=MessageDirection.OUTBOUND, run=blocking)

    result = await entry.run(request(direction=entry.direction))

    assert entry.step == PipelineStep.BLOCKLIST
    assert entry.direction == MessageDirection.OUTBOUND
    assert result.halts is True


# The same module runs on both directions, which is what distinguishes the two rows of blocklist and of guardrail.
async def test_the_same_step_runs_on_both_directions() -> None:
    inbound = Step(step=PipelineStep.GUARDRAIL, direction=MessageDirection.INBOUND, run=passing)
    outbound = Step(step=PipelineStep.GUARDRAIL, direction=MessageDirection.OUTBOUND, run=passing)

    assert inbound.step == outbound.step
    assert inbound.direction != outbound.direction
    assert (await outbound.run(request(direction=outbound.direction))).outcome == "pass"
