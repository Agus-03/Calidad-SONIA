# The third step of ADR-0016, and the first of the turn that costs a provider call: what a blocklist and a guardrail settle with
# a file, this one cannot — whether a question belongs to a business is a judgement about meaning, and the business is a
# different one for every agent (F01). What it compares against is the agent's objective component (decision D1): the domain of
# an agent is what its administrator wrote, so it travels as configuration and never as a list of topics in this repository.
#
# It runs after the guardrail and not before it, which is part of why the order of ADR-0016 is the order it is: this is the first
# text of the turn that leaves for a provider, and by here it is the redacted one (ADR-0006).
#
# Its verdict is the denominator of OE6, and that is the reason this step exists at all rather than letting the similarity score
# of the retrieval decide what counts as "within the domain" — a system that decides which questions it was supposed to know
# improves that metric exactly when it fails to recall (ADR-0016).

import json
import logging
import re
from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
from functools import lru_cache

from adapters.llm.base import LLMMessage, LLMRefusal
from infra.enums import PromptComponentType
from infra.rules import RulesFileError, load
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

# The second copy of this in the repository and it is deliberate: wrapping the answer in a fence is the commonest way a model
# ignores "sin bloques de código", and classifiers/intent.py carries the same three lines. A third copy is what should move them
# into one place — two is under the threshold where regla 2 says a shared module costs more than it saves.
_FENCE = re.compile(r"^\s*```(?:json)?\s*(.*?)\s*```\s*$", re.DOTALL)

# Enough of a bad answer to recognise it in a log, and it is admissible here for one reason: this step runs after the inbound
# guardrail, so whatever text reaches it has already been through the redaction of ADR-0006.
_MAX_REPORTED = 200


@dataclass(frozen=True)
class Rules:
    version: int
    marker: str
    system: str
    objective_heading: str
    user_heading: str


# Frozen because it is data on its way to a row, the same decision as the rest of the pipeline's dataclasses.
@dataclass(frozen=True)
class Verdict:
    label: str
    confidence: Decimal
    # Only evidence, never a decision: a greeting is in_domain like anything else this business attends, and this flag is what
    # lets the measurement of OE6 leave the greetings out of its denominator without a third value in the outcome domain.
    greeting: bool


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

    return Rules(
        version=spec.version,
        marker=marker,
        system=system,
        objective_heading=spec.text("objective"),
        user_heading=spec.text("user"),
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

    completion = await request.client.complete(
        model=request.turn.agent.llm_model,
        # The objective last, so the instruction that fixes the two labels is read before the text that describes the business.
        system=f"{spec.system}\n\n{spec.objective_heading}\n{objective}",
        messages=(LLMMessage(role="user", text=f"{spec.user_heading}\n{request.text.strip()}"),),
        # Zero so the same message triages the same way twice: this is a verdict a measurement rests on, not prose.
        temperature=0.0,
    )
    verdict = parse(completion.text)
    detail = {"greeting": verdict.greeting, "model": completion.model}

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


# Separate from the call so every case below can be tested without a model.
#
# Raises where the classifier of F11 returns nothing, and the difference between the two is what their answers are for: an
# intent nobody could read costs a row of evidence, and a verdict nobody could read decides whether a customer is answered at
# all. The domain of this step has no value for "could not decide", so returning in_domain here would write a row asserting a
# check that never happened — which is the thing pipeline_steps exists to prevent (ADR-0016).
def parse(answer: str) -> Verdict:
    text = answer.strip()
    if fenced := _FENCE.match(text):
        text = fenced.group(1)

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
    return Verdict(label=label, confidence=confidence, greeting=greeting is True)


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
