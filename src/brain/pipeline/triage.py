# The third step of ADR-0016 and the first that costs a provider call: whether a question belongs to a business is a judgement
# about meaning, and it compares against the agent's objective component and never a list of topics of ours (decision D1).
#
# It runs after the guardrail so the text that leaves for the provider is the redacted one (ADR-0006), and its verdict is the
# denominator of OE6 — which is why it exists instead of letting a similarity score decide what counted as in domain.

import json
import logging
from collections.abc import Mapping
from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
from functools import lru_cache
from typing import Any

from adapters.llm.base import LLMMessage, LLMRefusal, unfenced
from infra.enums import MessageDirection, PromptComponentType, SenderType, TransferSignalType
from infra.rules import RulesFileError, load
from pipeline.repository.turn import Message
from pipeline.repository.evidence import TracedStep
from pipeline.step import StepRequest, StepResult

logger = logging.getLogger("sonia.pipeline.triage")

_FILE = "triage.yaml"

# The outcome domain declared for this step in DER/entidades.md. There is no third value and no failed: a verdict nobody could
# read is not an outcome, and this module raises rather than inventing one.
_IN_DOMAIN = "in_domain"
_OUT_OF_DOMAIN = "out_of_domain"
_VERDICTS = frozenset({_IN_DOMAIN, _OUT_OF_DOMAIN})

# The scale of pipeline_steps.score, and enough for a number nobody compares beyond two digits.
_SCALE = Decimal("0.01")

# Enough of a bad answer to recognise it in a log, and it is admissible here for one reason: this step runs after the inbound
# guardrail, so whatever text reaches it has already been through the redaction of ADR-0006.
_MAX_REPORTED = 200

# The signals this call judges besides its own verdict (ADR-0045, section 2.1): the three whose default threshold the file keeps.
JUDGED = (TransferSignalType.OUT_OF_DOMAIN, TransferSignalType.EXPLICIT_REQUEST, TransferSignalType.NO_RESOLUTION)


@dataclass(frozen=True)
class Rules:
    version: int
    marker: str
    system: str
    objective_heading: str
    previous_heading: str
    user_heading: str
    # Used when the AIAgent has no policy for a signal, so threshold_used is a number somebody chose (ADR-0045, section 2.4).
    thresholds: Mapping[TransferSignalType, int]


# Frozen because it is data on its way to a row, the same decision as the rest of the pipeline's dataclasses.
@dataclass(frozen=True)
class Verdict:
    label: str
    confidence: Decimal
    # Only evidence, never a decision: a greeting is in_domain like anything else this business attends, and this flag is what
    # lets the measurement of OE6 leave the greetings out of its denominator without a third value in the outcome domain.
    greeting: bool
    # None is a signal nobody judged, which is written as skipped and never as a no (ADR-0045, section 2.2).
    explicit_request: bool | None = None
    resolved: bool | None = None


# Read once per process: the file is versioned data, so it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    spec = load(_FILE)
    marker = spec.text("marker")
    system = spec.text("system")
    # Checked while loading, because the doubled adapter finds this step by that literal: a prompt that stopped carrying it would
    # leave the doubled stack answering the triage with prose, every turn would fail, and nothing would say why.
    if marker not in system:
        raise RulesFileError(f"triage: the system prompt does not carry the marker {marker!r}")

    thresholds = spec.section("thresholds")
    return Rules(
        version=spec.version,
        marker=marker,
        system=system,
        objective_heading=spec.text("objective"),
        previous_heading=spec.text("previous"),
        user_heading=spec.text("user"),
        thresholds={signal: thresholds.integer(signal, minimum=1) for signal in JUDGED},
    )


async def run(request: StepRequest) -> StepResult:
    spec = rules()
    # varchar in the row, and for this step it is the file and not the model: what decided is the prompt, and regla 9 asks that
    # what decides be versioned data. The model that read it travels in the detail, where it is evidence and not an identifier.
    version = str(spec.version)

    objective = _objective(request)
    if objective is None:
        # The declared cost of D1, and it passes rather than refusing: an agent whose administrator never wrote what the
        # business does has nothing to compare against, and refusing everybody would leave that whole agent unable to answer.
        # The price is that every one of its turns enters the denominator of OE6 as a question of the domain.
        logger.warning("the triage had no objective to compare against", extra={"context": _context(request)})
        return StepResult(
            outcome=_IN_DOMAIN,
            version=version,
            text=request.text,
            detail={"objective": "missing"},
        )

    previous = last_answer(request.turn.history)
    user = f"{spec.user_heading}\n{request.text.strip()}"
    if previous is not None:
        # Before the message, in the order the conversation happened: the model judges the answer by what came after it.
        user = f"{spec.previous_heading}\n{previous.strip()}\n\n{user}"

    completion = await request.client.complete(
        model=request.turn.agent.llm_model,
        # The objective last, so the instruction that fixes the two labels is read before the text that describes the business.
        system=f"{spec.system}\n\n{spec.objective_heading}\n{objective}",
        messages=(LLMMessage(role="user", text=user),),
        # Zero so the same message triages the same way twice: this is a verdict a measurement rests on, not prose.
        temperature=0.0,
    )
    verdict = parse(completion.text)
    if previous is None:
        # No answer was shown, so whatever the model said about one is not a judgement of anything (ADR-0045, section 2.2).
        verdict = Verdict(verdict.label, verdict.confidence, verdict.greeting, verdict.explicit_request, None)
    detail = {
        "greeting": verdict.greeting,
        "model": completion.model,
        "explicit_request": verdict.explicit_request,
        "resolved": verdict.resolved,
    }

    if verdict.label == _OUT_OF_DOMAIN:
        # Ends the turn and nobody is told: the customer gets no automatic answer at all and the conversation goes to a person,
        # which is the same shape as the cut of 07-secuencia 4.1 and what the group decided for this one (O1c).
        logger.info("the triage sent the turn to a person", extra={"context": {**_context(request), **detail}})
        return StepResult(
            outcome=_OUT_OF_DOMAIN,
            version=version,
            text=request.text,
            halts=True,
            score=verdict.confidence,
            detail=detail,
        )

    # The text comes out exactly as it went in: this step decides and never rewrites, which is the guardrail's job.
    return StepResult(
        outcome=_IN_DOMAIN,
        version=version,
        text=request.text,
        score=verdict.confidence,
        detail=detail,
    )


# The answer no_resolution judges: the last thing that went out, and only if the AIAgent wrote it (ADR-0045, section 2.2).
def last_answer(history: tuple[Message, ...]) -> str | None:
    for message in reversed(history):
        if message.direction == MessageDirection.OUTBOUND:
            return message.content_text if message.sender_type == SenderType.AI_AGENT else None
    return None


# The verdict back from the row the step left, so the route reads what was traced and not a second copy (ADR-0045, section 2.1).
def verdict_of(step: TracedStep) -> Verdict | None:
    detail: dict[str, Any] = step.detail or {}
    if step.outcome not in _VERDICTS or detail.get("objective") == "missing":
        return None
    return Verdict(
        label=step.outcome,
        confidence=step.score if step.score is not None else Decimal(0),
        greeting=detail.get("greeting") is True,
        explicit_request=detail.get("explicit_request"),
        resolved=detail.get("resolved"),
    )


# Separate from the call so every case below can be tested without a model, and it raises where the classifier of F11 returns
# nothing: this domain has no value for "could not decide", and answering in_domain would assert a check that never ran.
def parse(answer: str) -> Verdict:
    text = unfenced(answer)

    try:
        payload = json.loads(text)
    except ValueError as err:
        raise LLMRefusal(f"triage: the model did not answer JSON: {text[:_MAX_REPORTED]}") from err
    if not isinstance(payload, dict):
        raise LLMRefusal(f"triage: the model answered JSON that is not an object: {text[:_MAX_REPORTED]}")

    label = payload.get("verdict")
    if label not in _VERDICTS:
        raise LLMRefusal(f"triage: {label!r} is not one of the two verdicts this step may write")

    try:
        # Through str(): a JSON float is already binary, and the column is numeric.
        confidence = Decimal(str(payload.get("confidence"))).quantize(_SCALE)
    except (InvalidOperation, ValueError, TypeError) as err:
        raise LLMRefusal(f"triage: {payload.get('confidence')!r} is not a confidence") from err
    if not Decimal(0) <= confidence <= Decimal(1):
        raise LLMRefusal(f"triage: a confidence of {confidence} is outside 0 and 1")

    # A missing flag is not worth losing a turn over: it is evidence for a measurement and not the verdict. The cost is that a
    # model which never emits it makes the exclusion of greetings from OE6 quietly empty, which is why it is asked for in the
    # prompt and asserted in the tests.
    greeting = payload.get("greeting")
    return Verdict(
        label=label,
        confidence=confidence,
        greeting=greeting is True,
        # Not a bool is not judged: writing it as false would put a no in the table OE3 reads (ADR-0045, section 2.2).
        explicit_request=_flag(payload.get("explicit_request")),
        resolved=_flag(payload.get("resolved")),
    )


def _flag(value: object) -> bool | None:
    return value if isinstance(value, bool) else None


# The one component this step reads, by type and not by position: F01 declares six and the administrator orders them, so the
# first one is whatever they dragged to the top.
def _objective(request: StepRequest) -> str | None:
    for component in request.components:
        if component.type == PromptComponentType.OBJECTIVE and component.content.strip():
            return component.content.strip()
    return None


# Never the text: this is the step that decides whether a person ever sees this conversation, and a log line is not where the
# message gets copied to (ADR-0006).
def _context(request: StepRequest) -> dict[str, str]:
    return {
        "conversation_id": str(request.turn.conversation_id),
        "message_batch_id": str(request.turn.message_batch_id),
        "ai_agent_id": str(request.turn.agent.id),
    }
