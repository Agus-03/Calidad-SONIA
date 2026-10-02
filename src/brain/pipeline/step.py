# The shape of one step of ADR-0016, written before any of them so the contract comes from the seven and not from whichever got
# built first. One value of PipelineStep is one module (regla 6), and one run of a module is one row of pipeline_steps.

from dataclasses import dataclass
from decimal import Decimal
from typing import Any, Protocol

from adapters.llm.base import LLMClient
from infra.enums import MessageDirection, PipelineStep
from pipeline.repository.turn import Turn
from prompts.repository import Component


# What a step is handed. Frozen because a step decides about it and never edits it: the text it returns is how it answers.
@dataclass(frozen=True)
class StepRequest:
    # The whole turn, because deciding can need the agent that owns it: triage calls the provider that ai_agents names, and a
    # blocklist does not care. A step that ignores these two is the normal case and not a sign of a wrong contract.
    turn: Turn
    client: LLMClient
    # What distinguishes the two runs of blocklist and of guardrail, and the only reason this is a field and not a constant.
    direction: MessageDirection
    # What this step decides about: the customer's message inbound, the answer the model produced outbound.
    text: str
    # The agent's active prompt components (F01), which is where triage finds what this business is about (decision D1). A field
    # and not a read of its own, because a step takes no connection and the route has already loaded them for the assembly.
    # Empty by default: most steps never look at them, and no step of the outbound leg does.
    components: tuple[Component, ...] = ()


# One row of pipeline_steps, minus what only the runner knows: position is the order in the turn and latency_ms is measured
# around the call, so neither is a step's to report.
@dataclass(frozen=True)
class StepResult:
    # Its domain is declared by step and not by an enumeration, which is why this is a string here and a varchar in the row:
    # C07 does not admit one whose set of values depends on another column (ADR-0016). DER/entidades.md has the seven tables.
    outcome: str
    # varchar and not integer: for a rules file it is its version field, and for a step that a model decides it is the model.
    version: str
    # What the next step works on, equal to the text that came in when this step changed nothing. Always present, so no reader
    # has to know that an absent text meant "unchanged" — the redaction of ADR-0006 is a step whose whole effect is here.
    text: str
    # The step's own decision to end the turn, and not the runner reading outcomes string by string: the domain of outcome
    # belongs to the step that produced it, so a runner that branched on it would have to know all seven.
    halts: bool = False
    # Only the steps that produce one: triage and grounding. Decimal because the column is numeric.
    score: Decimal | None = None
    # detail_json: which rule matched, and the top_k and the threshold of retrieval (H-23). Nullable, and never personal data.
    detail: dict[str, Any] | None = None


# A Protocol and not a base class, the same decision as LLMClient: a step is an async function in its own module, and nothing
# about it needs a class.
class StepFunc(Protocol):
    async def __call__(self, request: StepRequest) -> StepResult: ...


# One entry of the pipeline: what it writes into the two columns that name it, and what to call. The runner holds a sequence of
# these, which is what makes the order of the turn a piece of data and not a chain of calls.
@dataclass(frozen=True)
class Step:
    step: PipelineStep
    direction: MessageDirection
    run: StepFunc
