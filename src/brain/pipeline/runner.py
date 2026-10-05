# The runner of ADR-0016: it walks one leg, measures every step and writes nothing, so the caller persists the trace inside the
# transaction that holds the rest of the evidence. One leg and not the pipeline, because the turn is inbound leg, generation and
# outbound leg, and position is what keeps the three readable in order.

import logging
from collections.abc import Sequence
from dataclasses import dataclass
from time import perf_counter

from adapters.llm.base import LLMClient, LLMError
from infra.enums import MessageDirection, PipelineStep
from pipeline import blocklist, guardrail, triage
from pipeline.repository.turn import Turn
from pipeline.repository.evidence import TracedStep
from pipeline.step import Step, StepRequest
from prompts.repository import Component

logger = logging.getLogger("sonia.pipeline.runner")

# The inbound leg, in the order of ADR-0016. A tuple because the order of the turn is data: adding a step is adding an entry, and
# one row maps to one module (regla 6). retrieval and grounding come right after it but are not entries here: the route runs
# them once the signals are judged, because they need the searcher and an agent without a base writes no row (ADR-0074).
INBOUND: tuple[Step, ...] = (
    # First because it is the cheapest of the seven: a message with a forbidden term must not pay for a model to be refused.
    Step(step=PipelineStep.BLOCKLIST, direction=MessageDirection.INBOUND, run=blocklist.run),
    # Second, in the order of ADR-0016, and after the blocklist for a reason worth saying: a message that is going to be
    # refused outright should not be worth redacting first.
    Step(step=PipelineStep.GUARDRAIL, direction=MessageDirection.INBOUND, run=guardrail.run),
    # Third, and the first step of the turn that costs a provider call: whether a question belongs to a business is a judgement
    # about meaning, and no file settles it. After the guardrail on purpose — this is the first text that leaves for a provider,
    # and by here it is the redacted one (ADR-0006).
    Step(step=PipelineStep.TRIAGE, direction=MessageDirection.INBOUND, run=triage.run),
)

# The outbound leg: the same two steps about the answer the model produced, a second tuple because the text they decide about did
# not exist when the first ran. The generation takes the position between the two legs and the route writes it, so this leg starts
# one further along and no leg counts from one.
OUTBOUND: tuple[Step, ...] = (
    Step(step=PipelineStep.BLOCKLIST, direction=MessageDirection.OUTBOUND, run=blocklist.run),
    Step(step=PipelineStep.GUARDRAIL, direction=MessageDirection.OUTBOUND, run=guardrail.run),
)


# What one leg leaves behind. Frozen for the same reason as the results it holds: it is evidence on its way to a row.
@dataclass(frozen=True)
class Trace:
    # The rows to write, in the order they ran.
    steps: tuple[TracedStep, ...]
    # The text that came out the far end, which is not the one that went in: the redaction of ADR-0006 is a step whose whole
    # effect is this field, and it is this text and not the original that has to reach the model.
    text: str
    # Where the next leg starts numbering, so position stays continuous across the whole turn and not inside each leg.
    next_position: int
    # The step that ended the turn, if one did. The caller answers blocked off this and never off an outcome, because the domain
    # of outcome is declared per step (ADR-0016) and a reader of it would have to know all seven.
    halted_at: Step | None = None

    @property
    def halted(self) -> bool:
        return self.halted_at is not None


# Raised when a step could not decide at all, which no declared outcome domain has a value for: blocklist, guardrail and triage
# have no failed. It carries what ran before, because 07-secuencia section 5 keeps the partial evidence of a turn that fell over
# — it is the record of what did manage to happen, and the caller is the one that writes it.
class StepFailed(RuntimeError):
    def __init__(self, step: Step, trace: Trace, cause: LLMError) -> None:
        super().__init__(f"pipeline: {step.step} ({step.direction}) could not decide: {cause}")
        self.step = step
        self.trace = trace


# Takes no connection: a step can spend seconds against a provider, and holding a pool connection through that starves the next
# turn. The caller opens one afterwards and writes the trace with the rest of the evidence.
async def run(
    *,
    steps: Sequence[Step],
    turn: Turn,
    client: LLMClient,
    text: str,
    start_position: int = 1,
    components: tuple[Component, ...] = (),
) -> Trace:
    traced: list[TracedStep] = []
    position = start_position

    for step in steps:
        request = StepRequest(turn=turn, client=client, direction=step.direction, text=text, components=components)

        # Measured here and not inside the step, so no step can disagree with the trace it is part of. perf_counter because a
        # wall clock that steps back mid-turn would report a negative latency.
        started = perf_counter()
        try:
            result = await step.run(request)
        except LLMError as err:
            # Only what a provider can do to us. A bug in a step is not an outcome and is not dressed up as one: it propagates,
            # and the caller reports a failed turn.
            raise StepFailed(step, _trace(traced, text, position), err) from err
        latency_ms = round((perf_counter() - started) * 1000)

        traced.append(
            TracedStep(
                step=step.step,
                direction=step.direction,
                position=position,
                outcome=result.outcome,
                version=result.version,
                latency_ms=latency_ms,
                score=result.score,
                detail=result.detail,
            )
        )
        # What the next step decides about, which is this one's answer and not what the leg was handed.
        text = result.text
        position += 1

        if result.halts:
            logger.info(
                "the pipeline cut the turn",
                extra={
                    "context": {
                        "conversation_id": str(turn.conversation_id),
                        "message_batch_id": str(turn.message_batch_id),
                        "step": step.step,
                        "direction": step.direction,
                        "position": traced[-1].position,
                        "outcome": result.outcome,
                    }
                },
            )
            return _trace(traced, text, position, halted_at=step)

    return _trace(traced, text, position)


def _trace(steps: list[TracedStep], text: str, position: int, halted_at: Step | None = None) -> Trace:
    return Trace(steps=tuple(steps), text=text, next_position=position, halted_at=halted_at)
