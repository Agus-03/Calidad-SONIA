# The runner of ADR-0016 and the trace it leaves: a turn has to be readable in order, with its times, and it has to be readable
# above all when the pipeline cut it — those are the rows that exist precisely because there is no generation to look at.

from datetime import UTC, datetime
from decimal import Decimal
from uuid import uuid4

import pytest

from adapters.llm.base import Completion, LLMTimeout
from infra.enums import MessageDirection, PipelineStep, SenderType
from pipeline import runner
from pipeline.repository import Agent, Message, TracedStep, Turn, write_steps
from pipeline.step import Step, StepRequest, StepResult

BATCH_ID = uuid4()


class FakeClient:
    provider = "double"

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        return Completion(text="listo", model=model, prompt_tokens=1, completion_tokens=1)


# Records what was sent and not what a database would answer: every case here is about the statements, not about their result.
class RecordingConnection:
    def __init__(self) -> None:
        self.calls: list[tuple[str, dict]] = []

    async def execute(self, sql: str, params: dict | None = None):
        self.calls.append((sql, params or {}))
        return None


def turn() -> Turn:
    return Turn(
        conversation_id=uuid4(),
        message_batch_id=BATCH_ID,
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


# Built out of what the case is about, so no test has to write a module to assert one thing about the walk.
def step(
    value: PipelineStep,
    *,
    direction: MessageDirection = MessageDirection.INBOUND,
    outcome: str = "pass",
    halts: bool = False,
    text: str | None = None,
    score: Decimal | None = None,
    detail: dict | None = None,
    ran: list[PipelineStep] | None = None,
    raises: Exception | None = None,
) -> Step:
    async def run(request: StepRequest) -> StepResult:
        if ran is not None:
            ran.append(value)
        if raises is not None:
            raise raises
        return StepResult(
            outcome=outcome,
            version="1",
            text=request.text if text is None else text,
            halts=halts,
            score=score,
            detail=detail,
        )

    return Step(step=value, direction=direction, run=run)


async def walk(*steps: Step, text: str = "hola", start_position: int = 1) -> runner.Trace:
    return await runner.run(
        steps=steps, turn=turn(), client=FakeClient(), text=text, start_position=start_position
    )


# The order of the turn is data, so it is read here and not inferred from a chain of calls. The blocklist opens the leg because
# it is the cheapest of the seven: what can be refused without a provider is refused before one gets paid (ADR-0016).
def test_the_inbound_leg_opens_with_the_blocklist() -> None:
    assert [s.step for s in runner.INBOUND] == [PipelineStep.BLOCKLIST]
    assert [s.direction for s in runner.INBOUND] == [MessageDirection.INBOUND]


async def test_a_leg_with_no_steps_changes_nothing() -> None:
    trace = await walk(text="hola")

    assert trace.steps == ()
    assert trace.text == "hola"
    assert trace.halted is False
    assert trace.next_position == 1


# position is the order inside the turn and not inside the leg, which is what lets the second leg keep counting (ADR-0016).
async def test_position_counts_from_where_the_previous_leg_stopped() -> None:
    trace = await walk(
        step(PipelineStep.BLOCKLIST, direction=MessageDirection.OUTBOUND),
        step(PipelineStep.GUARDRAIL, direction=MessageDirection.OUTBOUND),
        start_position=4,
    )

    assert [s.position for s in trace.steps] == [4, 5]
    assert trace.next_position == 6


async def test_the_trace_reads_in_the_order_the_steps_ran() -> None:
    trace = await walk(
        step(PipelineStep.BLOCKLIST),
        step(PipelineStep.GUARDRAIL),
        step(PipelineStep.TRIAGE, outcome="in_domain", score=Decimal("0.94")),
    )

    assert [s.step for s in trace.steps] == [PipelineStep.BLOCKLIST, PipelineStep.GUARDRAIL, PipelineStep.TRIAGE]
    assert [s.position for s in trace.steps] == [1, 2, 3]
    assert trace.steps[-1].score == Decimal("0.94")


# Measured by the runner, which is why a step has no field for it: latency per step is what says which stretch is the slow one.
async def test_every_step_is_timed_by_the_runner() -> None:
    trace = await walk(step(PipelineStep.BLOCKLIST), step(PipelineStep.TRIAGE))

    assert all(s.latency_ms >= 0 for s in trace.steps)
    assert all(isinstance(s.latency_ms, int) for s in trace.steps)


# The cut is the whole reason the runner exists: what comes after a block must not run, and least of all pay for a model.
async def test_a_cut_stops_the_leg_and_nothing_after_it_runs() -> None:
    ran: list[PipelineStep] = []
    trace = await walk(
        step(PipelineStep.BLOCKLIST, outcome="blocked", halts=True, detail={"term": "prohibido"}, ran=ran),
        step(PipelineStep.GUARDRAIL, ran=ran),
        step(PipelineStep.TRIAGE, ran=ran),
    )

    assert ran == [PipelineStep.BLOCKLIST], "the pipeline kept going after it had already cut the turn"
    assert trace.halted is True
    assert trace.halted_at is not None and trace.halted_at.step == PipelineStep.BLOCKLIST
    assert len(trace.steps) == 1
    assert trace.steps[0].detail == {"term": "prohibido"}


# The runner obeys the field and never reads the outcome: its domain is declared per step, so a runner that branched on it would
# have to know all seven and would break the day an eighth appears.
async def test_the_runner_obeys_halts_and_not_the_outcome() -> None:
    trace = await walk(step(PipelineStep.GUARDRAIL, outcome="blocked", halts=False), step(PipelineStep.TRIAGE))

    assert trace.halted is False, "the runner decided by itself what an outcome means"
    assert len(trace.steps) == 2


# The redaction of ADR-0006 is a step whose whole effect is the text, so what the next one decides about is this one's answer.
async def test_the_text_of_one_step_is_what_the_next_one_decides_about() -> None:
    seen: list[str] = []

    async def recording(request: StepRequest) -> StepResult:
        seen.append(request.text)
        return StepResult(outcome="pass", version="1", text=request.text)

    trace = await walk(
        step(PipelineStep.GUARDRAIL, outcome="modified", text="te paso mi CBU [CBU]"),
        Step(step=PipelineStep.TRIAGE, direction=MessageDirection.INBOUND, run=recording),
    )

    assert seen == ["te paso mi CBU [CBU]"], "the second step decided about the text the first one replaced"
    assert trace.text == "te paso mi CBU [CBU]"


# What 07-secuencia section 5 asks for: the evidence of what did manage to happen survives the step that fell over.
async def test_a_provider_failure_keeps_the_evidence_of_what_already_ran() -> None:
    with pytest.raises(runner.StepFailed) as raised:
        await walk(
            step(PipelineStep.BLOCKLIST),
            step(PipelineStep.TRIAGE, raises=LLMTimeout("double: no answer in 10s")),
        )

    failure = raised.value
    assert failure.step.step == PipelineStep.TRIAGE
    # No row for the step that failed: no declared outcome domain has a value for "could not decide" (ADR-0016).
    assert [s.step for s in failure.trace.steps] == [PipelineStep.BLOCKLIST]
    assert "triage" in str(failure)


# A bug in a step is not an outcome and is not dressed up as one: only what a provider can do to us becomes StepFailed.
async def test_a_bug_in_a_step_is_not_reported_as_a_decision() -> None:
    with pytest.raises(ZeroDivisionError):
        await walk(step(PipelineStep.TRIAGE, raises=ZeroDivisionError("a bug")))


# The write: nothing is sent for an empty trace, which is the case of every turn until the steps of this phase exist.
async def test_an_empty_trace_sends_no_statement() -> None:
    conn = RecordingConnection()

    await write_steps(conn, BATCH_ID, ())

    assert conn.calls == [], "a turn with no steps issued a statement nobody asked for"


# Deleted first, like the intents: the core asks again when the brain answers a 5xx, and a second run has to leave one trace.
async def test_the_trace_of_a_batch_is_replaced_and_not_added_to() -> None:
    conn = RecordingConnection()

    await write_steps(conn, BATCH_ID, (_row(1), _row(2)))

    assert "DELETE" in conn.calls[0][0]
    assert conn.calls[0][1] == {"message_batch_id": BATCH_ID}
    assert len([sql for sql, _ in conn.calls if "INSERT" in sql]) == 2
    assert [params["position"] for _, params in conn.calls[1:]] == [1, 2]


# Wrapped because psycopg does not adapt a dict on its own, and NULL where there is no detail: an empty object would read as a
# step that reported nothing, which is not the same as one that had nothing to report.
async def test_the_detail_travels_as_jsonb_and_an_absent_one_as_null() -> None:
    conn = RecordingConnection()

    await write_steps(conn, BATCH_ID, (_row(1, detail={"top_k": 5}), _row(2)))

    with_detail = conn.calls[1][1]
    without = conn.calls[2][1]
    assert with_detail["detail_json"].obj == {"top_k": 5}
    assert without["detail_json"] is None


def _row(position: int, detail: dict | None = None) -> TracedStep:
    return TracedStep(
        step=PipelineStep.BLOCKLIST,
        direction=MessageDirection.INBOUND,
        position=position,
        outcome="pass",
        version="1",
        latency_ms=3,
        detail=detail,
    )
