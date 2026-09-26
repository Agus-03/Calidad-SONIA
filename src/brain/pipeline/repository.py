# The turn's reads, next to what uses them: the brain gets four ids and finds everything else here (07-secuencia 1).

import logging
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from datetime import datetime
from types import MappingProxyType
from decimal import Decimal
from typing import Any
from uuid import UUID

from psycopg import AsyncConnection
from psycopg.types.json import Jsonb

from classifiers.intent import Intent
from infra import ids
from infra.enums import (
    GenerationStatus,
    MessageDirection,
    PipelineStep,
    SenderType,
    TransferSignalType,
)

logger = logging.getLogger("sonia.pipeline.repository")

# Moves to Settings with the rest of the token budget in the config task; a literal here would outlive that.
DEFAULT_HISTORY_MESSAGES = 20


# Raised when the four ids do not name a turn, which is a caller mistake and not a failure of ours.
class TurnNotFound(Exception):
    def __init__(self, message_batch_id: UUID) -> None:
        super().__init__(f"no turn for message_batch_id {message_batch_id}")
        self.message_batch_id = message_batch_id


# Frozen because a port is data and not state: the same decision as the LLM dataclasses.
@dataclass(frozen=True)
class Agent:
    id: UUID
    name: str
    llm_provider: str
    llm_model: str
    temperature: float | None
    max_tokens: int | None


@dataclass(frozen=True)
class Message:
    direction: MessageDirection
    sender_type: SenderType
    content_text: str
    sent_at: datetime


@dataclass(frozen=True)
class Turn:
    conversation_id: UUID
    message_batch_id: UUID
    turn_number: int
    agent: Agent
    # What arrived in this batch, which is what has to be answered.
    incoming: tuple[Message, ...]
    # What came before it, oldest first, so a prompt reads in the order the conversation happened.
    history: tuple[Message, ...]


_BATCH = """
SELECT b.id, b.conversation_id, b.turn_number
FROM   message_batches b
WHERE  b.id = %(message_batch_id)s AND b.conversation_id = %(conversation_id)s
"""

_AGENT = """
SELECT a.id, a.name, a.llm_provider, a.llm_model, a.temperature, a.max_tokens
FROM   ai_agents a
WHERE  a.id = %(ai_agent_id)s AND a.deleted_at IS NULL
"""

# Ordered by the channel's clock and not by insertion, because C06 makes sent_at the shared reading of when it happened.
_INCOMING = """
SELECT m.direction, m.sender_type, m.content_text, m.sent_at
FROM   messages m
WHERE  m.message_batch_id = %(message_batch_id)s
ORDER  BY m.sent_at, m.created_at
"""

# Everything before this batch, newest first so the limit keeps the recent turns, and reversed by the caller.
_HISTORY = """
SELECT m.direction, m.sender_type, m.content_text, m.sent_at
FROM   messages m
WHERE  m.conversation_id = %(conversation_id)s
  AND  (m.message_batch_id IS DISTINCT FROM %(message_batch_id)s)
  AND  m.hidden_at IS NULL
ORDER  BY m.sent_at DESC, m.created_at DESC
LIMIT  %(limit)s
"""


# Async because every statement here is I/O, and it takes the connection so the caller owns the transaction.
async def read_turn(
    conn: AsyncConnection,
    message_batch_id: UUID,
    conversation_id: UUID,
    ai_agent_id: UUID,
    history_limit: int = DEFAULT_HISTORY_MESSAGES,
) -> Turn:
    cursor = await conn.execute(
        _BATCH, {"message_batch_id": message_batch_id, "conversation_id": conversation_id}
    )
    batch = await cursor.fetchone()
    if batch is None:
        raise TurnNotFound(message_batch_id)

    cursor = await conn.execute(_AGENT, {"ai_agent_id": ai_agent_id})
    agent_row = await cursor.fetchone()
    if agent_row is None:
        raise TurnNotFound(message_batch_id)

    cursor = await conn.execute(_INCOMING, {"message_batch_id": message_batch_id})
    incoming = [_message(row) for row in await cursor.fetchall()]

    cursor = await conn.execute(
        _HISTORY,
        {
            "conversation_id": conversation_id,
            "message_batch_id": message_batch_id,
            "limit": history_limit,
        },
    )
    history = [_message(row) for row in await cursor.fetchall()]

    return Turn(
        conversation_id=conversation_id,
        message_batch_id=message_batch_id,
        turn_number=batch["turn_number"],
        agent=_agent(agent_row),
        incoming=tuple(incoming),
        history=tuple(reversed(history)),
    )


def _agent(row: dict) -> Agent:
    return Agent(
        id=row["id"],
        name=row["name"],
        llm_provider=row["llm_provider"],
        llm_model=row["llm_model"],
        # numeric arrives as Decimal, and the LLM adapters take a float; the money rule of C08 is about cost, not temperature.
        temperature=float(row["temperature"]) if row["temperature"] is not None else None,
        max_tokens=row["max_tokens"],
    )


def _message(row: dict) -> Message:
    return Message(
        direction=MessageDirection(row["direction"]),
        sender_type=SenderType(row["sender_type"]),
        content_text=row["content_text"],
        sent_at=row["sent_at"],
    )


# What the administrator decided about one of the four signals of F12. Read and never written here: transfer_policies is
# configuration of the core (docs/struct.md), and regla 3 lets the brain write evidence and nothing else.
@dataclass(frozen=True)
class TransferPolicy:
    enabled: bool
    threshold: Decimal


# Oldest first, so the newest row of a signal wins below: nothing in 0001 makes (ai_agent_id, signal_type) unique.
_POLICIES = """
SELECT signal_type, enabled, threshold
  FROM transfer_policies
 WHERE ai_agent_id = %(ai_agent_id)s
   AND deleted_at IS NULL
 ORDER BY created_at, id
"""


# A signal missing from the answer has no policy, which is not the same as one that is disabled: nobody configured it. Either way
# it proposes nothing — what is lost is the transfer_events row, which is the escalation F36 counts (ADR-0045, section 2.4).
async def read_transfer_policies(
    conn: AsyncConnection, ai_agent_id: UUID
) -> dict[TransferSignalType, TransferPolicy]:
    cursor = await conn.execute(_POLICIES, {"ai_agent_id": ai_agent_id})
    return {
        TransferSignalType(row["signal_type"]): TransferPolicy(enabled=row["enabled"], threshold=row["threshold"])
        for row in await cursor.fetchall()
    }


# The run of the batch just before this one, which only continues under the same prompt and when that batch did not end in a
# transfer: a conversation an Operator turned back on starts again from zero (ADR-0045, section 2.2).
_NO_RESOLUTION_STREAK = """
SELECT se.score
  FROM signal_evaluations se
  JOIN message_batches b ON b.id = se.message_batch_id
 WHERE b.conversation_id = %(conversation_id)s
   AND b.turn_number = %(turn_number)s - 1
   AND se.signal_type = %(signal_type)s
   AND se.classifier_version = %(classifier_version)s
   AND NOT EXISTS (
       SELECT 1 FROM transfer_events te
        WHERE te.conversation_id = b.conversation_id AND te.turn_number = b.turn_number)
"""


async def read_no_resolution_streak(conn: AsyncConnection, turn: Turn, classifier_version: str) -> int:
    cursor = await conn.execute(
        _NO_RESOLUTION_STREAK,
        {
            "conversation_id": turn.conversation_id,
            "turn_number": turn.turn_number,
            "signal_type": TransferSignalType.NO_RESOLUTION,
            "classifier_version": classifier_version,
        },
    )
    row = await cursor.fetchone()
    return int(row["score"]) if row is not None else 0


# The dynamic context of F04 as the prompt reads it: the columns of conversation_contexts, plus the flag of ADR-0023 that only
# the version keeps. Frozen because it is data on its way to a row.
@dataclass(frozen=True)
class Context:
    summary: str | None
    # The dominant label of this batch's detected_intents, which is what DER/entidades.md makes it: never the rewriter's.
    primary_intent: str | None
    objections: tuple[str, ...]
    urgency_level: int | None
    budget_signal: str | None
    # Carried as it was: ADR-0047 sends the contact's data to contact_attributes and the commercial ones to the columns above.
    extracted_data: Mapping[str, Any] = MappingProxyType({})
    # None is a context nobody judged, which is the one read back from conversation_contexts: that table has no column for it.
    intent_changed: bool | None = None


# The context the turn starts from, and the version the core copied it from, which is what a generation points at when the
# rewrite of this turn fails (03-capa-prompts, Regla 3; ADR-0048).
@dataclass(frozen=True)
class CurrentContext:
    turn_number: int
    context: Context
    version_id: UUID | None


# One rewrite as it ran: the evidence of F04, and an evidence table since ADR-0048 amended regla 3.
@dataclass(frozen=True)
class ContextVersion:
    id: UUID
    conversation_id: UUID
    turn_number: int
    context: Context
    rewritten_by_model: str
    latency_ms: int


# conversation_contexts is written by the core, in the transaction that closes the turn, so what is read here is the context of
# the last turn that finished and not of the last one that was attempted (ADR-0048).
_CONTEXT = """
SELECT c.turn_number, c.summary, c.primary_intent, c.objections_json, c.urgency_level, c.budget_signal, c.extracted_data_json
FROM   conversation_contexts c
WHERE  c.conversation_id = %(conversation_id)s
"""

# The newest of that turn, because a turn the core asked for twice on a 5xx (ADR-0020) leaves two versions of the same number.
_CONTEXT_VERSION = """
SELECT v.id
FROM   conversation_context_versions v
WHERE  v.conversation_id = %(conversation_id)s AND v.turn_number = %(turn_number)s
ORDER  BY v.created_at DESC, v.id DESC
LIMIT  1
"""


# None on the first turn of a conversation, which has no row yet (03-capa-prompts, Regla 3).
async def read_context(conn: AsyncConnection, conversation_id: UUID) -> CurrentContext | None:
    cursor = await conn.execute(_CONTEXT, {"conversation_id": conversation_id})
    row = await cursor.fetchone()
    if row is None:
        return None

    cursor = await conn.execute(
        _CONTEXT_VERSION, {"conversation_id": conversation_id, "turn_number": row["turn_number"]}
    )
    version = await cursor.fetchone()
    return CurrentContext(
        turn_number=row["turn_number"],
        context=Context(
            summary=row["summary"],
            primary_intent=row["primary_intent"],
            objections=tuple(o for o in row["objections_json"] or () if isinstance(o, str)),
            urgency_level=row["urgency_level"],
            budget_signal=row["budget_signal"],
            extracted_data=MappingProxyType(dict(row["extracted_data_json"] or {})),
        ),
        version_id=version["id"] if version is not None else None,
    )


# The keys are the columns of conversation_contexts, because the core copies this object into that row with SQL and the two
# have to agree on the names (ADR-0048).
def content_of(context: Context) -> dict[str, Any]:
    return {
        "summary": context.summary,
        "primary_intent": context.primary_intent,
        "objections_json": list(context.objections),
        "urgency_level": context.urgency_level,
        "budget_signal": context.budget_signal,
        "extracted_data_json": dict(context.extracted_data),
        "intent_changed": context.intent_changed,
    }


_CONTEXT_VERSION_ROW = """
INSERT INTO conversation_context_versions (id, conversation_id, turn_number, content, rewritten_by_model, latency_ms)
VALUES (%(id)s, %(conversation_id)s, %(turn_number)s, %(content)s, %(rewritten_by_model)s, %(latency_ms)s)
"""


# Never deleted first, unlike the steps: a version is immutable (DER/entidades.md) and a generation may already point at it.
async def write_context_version(conn: AsyncConnection, version: ContextVersion) -> None:
    await conn.execute(
        _CONTEXT_VERSION_ROW,
        {
            "id": version.id,
            "conversation_id": version.conversation_id,
            "turn_number": version.turn_number,
            "content": Jsonb(content_of(version.context)),
            "rewritten_by_model": version.rewritten_by_model,
            "latency_ms": version.latency_ms,
        },
    )


# The write: the only INSERT of a turn, and generations is an evidence table, the only kind the brain may write (regla 3).


# One turn's evidence, as 07-secuencia section 2 declares it.
@dataclass(frozen=True)
class Generation:
    id: UUID
    conversation_id: UUID
    ai_agent_id: UUID
    message_batch_id: UUID
    # Which client actually answered, so a doubled run is recognisable by its own provider name and excludable from a cost.
    llm_provider: str
    llm_model: str
    prompt_rendered: str
    completion_text: str
    prompt_tokens: int
    completion_tokens: int
    latency_ms: int
    # Decimal and never float: money is numeric everywhere, in the database and in both languages (C08).
    cost: Decimal
    currency: str
    status: GenerationStatus
    # The version of each component that went in, in the order it went in: the "assembled" side of OE1 (ADR-0015).
    component_version_ids: tuple[UUID, ...]
    # When those versions were read, which is the instant OE1 measures against (ADR-0039, regla 10).
    assembled_at: datetime
    # The context this answer was given: this turn's rewrite, or the last one when it failed, or none on a first turn (ADR-0048).
    context_version_id: UUID | None = None


# message_id is absent on purpose: the core links it inside the transaction of section 2 (ADR-0017 (i).2).
_GENERATION = """
INSERT INTO generations (
    id, conversation_id, ai_agent_id, message_batch_id,
    llm_provider, llm_model, prompt_rendered, completion_text,
    prompt_tokens, completion_tokens, latency_ms, cost, currency, status, assembled_at, conversation_context_version_id
) VALUES (
    %(id)s, %(conversation_id)s, %(ai_agent_id)s, %(message_batch_id)s,
    %(llm_provider)s, %(llm_model)s, %(prompt_rendered)s, %(completion_text)s,
    %(prompt_tokens)s, %(completion_tokens)s, %(latency_ms)s, %(cost)s, %(currency)s, %(status)s, %(assembled_at)s,
    %(context_version_id)s
)
"""


_GENERATION_COMPONENT = """
INSERT INTO generation_components (id, generation_id, prompt_component_version_id, position)
VALUES (%(id)s, %(generation_id)s, %(version_id)s, %(position)s)
"""


# Takes the connection like every read here, so the caller keeps owning the transaction (regla 5 on the Python side).
# The generation and what it assembled go in the same transaction: a generation without its components reads as 0 % in OE1.
async def write_generation(conn: AsyncConnection, generation: Generation) -> None:
    await conn.execute(
        _GENERATION,
        {
            "id": generation.id,
            "conversation_id": generation.conversation_id,
            "ai_agent_id": generation.ai_agent_id,
            "message_batch_id": generation.message_batch_id,
            "llm_provider": generation.llm_provider,
            "llm_model": generation.llm_model,
            "prompt_rendered": generation.prompt_rendered,
            "completion_text": generation.completion_text,
            "prompt_tokens": generation.prompt_tokens,
            "completion_tokens": generation.completion_tokens,
            "latency_ms": generation.latency_ms,
            "cost": generation.cost,
            "currency": generation.currency,
            "status": generation.status,
            "assembled_at": generation.assembled_at,
            "context_version_id": generation.context_version_id,
        },
    )
    # One statement per component and not executemany: they are six at most, and the order of the rows is the order of the prompt.
    for position, version_id in enumerate(generation.component_version_ids, start=1):
        await conn.execute(
            _GENERATION_COMPONENT,
            {
                "id": ids.new(),
                "generation_id": generation.id,
                "version_id": version_id,
                "position": position,
            },
        )


# detected_intents is the evidence of F11, and one of the six tables the brain may write (regla 3).
_INTENTS_OF_BATCH = "DELETE FROM detected_intents WHERE message_batch_id = %(message_batch_id)s"

_INTENT = """
INSERT INTO detected_intents (id, message_batch_id, intent_label, confidence, position)
VALUES (%(id)s, %(message_batch_id)s, %(intent_label)s, %(confidence)s, %(position)s)
"""


# Deleted first and not upserted: the core asks again when the brain answers a 5xx (ADR-0020), and a second run of the same
# batch has to leave one classification and not two. There is no natural key to upsert on, because the labels are free text.
async def write_intents(
    conn: AsyncConnection, message_batch_id: UUID, intents: Sequence[Intent]
) -> None:
    await conn.execute(_INTENTS_OF_BATCH, {"message_batch_id": message_batch_id})
    # One statement per intent and not executemany: they are five at most, and the order of the rows is the order of the asking.
    for intent in intents:
        await conn.execute(
            _INTENT,
            {
                "id": ids.new(),
                "message_batch_id": message_batch_id,
                "intent_label": intent.label,
                "confidence": intent.confidence,
                "position": intent.position,
            },
        )


# pipeline_steps is the trace of ADR-0016, and the first of the six tables the brain writes that can outlive the answer: a turn
# the pipeline cut has these rows and no generation at all, which is why it hangs from message_batches and not from generations.


# One step as it ran. It restates the fields of StepResult instead of holding one, because pipeline/step.py needs Turn from this
# module and importing it back from here would close the cycle. The runner is what maps one to the other.
@dataclass(frozen=True)
class TracedStep:
    step: PipelineStep
    # What tells the two runs of blocklist and of guardrail apart (ADR-0016).
    direction: MessageDirection
    # The order of execution inside the turn, from 1 and continuous across both directions.
    position: int
    # Its domain is declared by step and is not an enumeration: C07 does not admit one that depends on another column.
    outcome: str
    # The version of the list, the rule or the model that decided. Without it, adjusting a guardrail invalidates every earlier
    # measurement without saying so.
    version: str
    # Measured by the runner around the call, so a step cannot disagree with the trace it is part of.
    latency_ms: int
    # Only the steps that produce one: triage and grounding.
    score: Decimal | None = None
    detail: dict | None = None


# One evaluated signal of F12, whether it fired or not. That is the whole point of the table: a classifier whose precision is an
# objective has to persist its output even when nothing happened, or the only sample left is the one the threshold already filtered.
@dataclass(frozen=True)
class SignalEvaluation:
    id: UUID
    message_batch_id: UUID
    signal_type: TransferSignalType
    # 1 or 0 for a yes/no signal, and the run of turns for no_resolution (ADR-0045, section 2.4).
    score: Decimal
    # What score was compared against, frozen so moving the threshold later does not rewrite what was measured: 1 for a yes/no
    # signal, the policy's for no_resolution, or the default of triage.yaml when there is no policy (ADR-0045, section 2.4).
    threshold_used: Decimal
    classifier_version: str
    # Not derivable from score and threshold: a signal can be over the line and not fire, because the policy is disabled or
    # because there is no policy at all.
    fired: bool


_SIGNALS_OF_BATCH = """
DELETE FROM signal_evaluations
 WHERE message_batch_id = %(message_batch_id)s
   AND signal_type = %(signal_type)s
"""

_SIGNAL = """
INSERT INTO signal_evaluations (
    id, message_batch_id, signal_type, score, threshold_used, classifier_version, fired
) VALUES (
    %(id)s, %(message_batch_id)s, %(signal_type)s, %(score)s, %(threshold_used)s, %(classifier_version)s, %(fired)s
)
"""


# Deleted first like the steps and the intents: the core asks again on a 5xx (ADR-0020) and one turn leaves one evaluation per
# signal. By signal_type, because the other three of F12 belong to their own classifiers. A row transfer_events points at cannot
# be deleted, and that FK is doing its job: this turn fails loudly instead of duplicating evidence.
async def write_signal_evaluation(conn: AsyncConnection, evaluation: SignalEvaluation) -> None:
    await conn.execute(
        _SIGNALS_OF_BATCH,
        {"message_batch_id": evaluation.message_batch_id, "signal_type": evaluation.signal_type},
    )
    await conn.execute(
        _SIGNAL,
        {
            "id": evaluation.id,
            "message_batch_id": evaluation.message_batch_id,
            "signal_type": evaluation.signal_type,
            "score": evaluation.score,
            "threshold_used": evaluation.threshold_used,
            "classifier_version": evaluation.classifier_version,
            "fired": evaluation.fired,
        },
    )


_STEPS_OF_BATCH = "DELETE FROM pipeline_steps WHERE message_batch_id = %(message_batch_id)s"

_STEP = """
INSERT INTO pipeline_steps (
    id, message_batch_id, step, direction, position, outcome, score, version, latency_ms, detail_json
) VALUES (
    %(id)s, %(message_batch_id)s, %(step)s, %(direction)s, %(position)s, %(outcome)s, %(score)s, %(version)s,
    %(latency_ms)s, %(detail_json)s
)
"""


# Deleted first and not upserted, for the same reason as the intents: the core asks again when the brain answers a 5xx
# (ADR-0020), and a second run of the same batch has to leave one trace and not two interleaved by position.
# Nothing is sent when the trace is empty: a DELETE per turn that can match nothing is a statement nobody asked for.
async def write_steps(
    conn: AsyncConnection, message_batch_id: UUID, steps: Sequence[TracedStep]
) -> None:
    if not steps:
        return

    await conn.execute(_STEPS_OF_BATCH, {"message_batch_id": message_batch_id})
    # One statement per row and not executemany: they are nine at most, and the order of the rows is the order of the turn.
    for step in steps:
        await conn.execute(
            _STEP,
            {
                "id": ids.new(),
                "message_batch_id": message_batch_id,
                "step": step.step,
                "direction": step.direction,
                "position": step.position,
                "outcome": step.outcome,
                "score": step.score,
                "version": step.version,
                "latency_ms": step.latency_ms,
                # Wrapped because psycopg does not adapt a dict on its own, and NULL and not an empty object when there is no
                # detail: {} would read as "the step reported nothing" where NULL reads as "the step had nothing to report".
                "detail_json": Jsonb(step.detail) if step.detail is not None else None,
            },
        )
