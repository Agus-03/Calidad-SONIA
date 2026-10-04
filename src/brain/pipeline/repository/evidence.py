# The evidence of a turn the brain writes: the generation, the intents of F11 and the trace of ADR-0016 (regla 3).

from collections.abc import Sequence
from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal
from uuid import UUID

from psycopg import AsyncConnection
from psycopg.types.json import Jsonb

from classifiers.intent import Intent
from infra import ids
from infra.enums import GenerationStatus, MessageDirection, PipelineStep


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


# One chunk the retrieval step found, as retrieval_logs keeps it: every one, with no threshold, so the threshold can be swept later
# with what is already in the table (ADR-0068, regla 3; H-23).
@dataclass(frozen=True)
class RetrievedChunk:
    document_chunk_id: UUID
    similarity: Decimal
    rank: int


# One step as it ran. It restates the fields of StepResult instead of holding one, because pipeline/step.py needs Turn from this
# package and importing it back from here would close the cycle. The runner is what maps one to the other.
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
    # Only the retrieval step has any: its rows of retrieval_logs, which hang from this row and not from the generation, so a turn
    # the grounding cut keeps what it retrieved (ADR-0068, regla 6).
    retrieved: tuple[RetrievedChunk, ...] = ()


# The logs go with the steps they hang from, in the same statement: a second run of the batch could not delete its trace with
# them still pointing at it. The foreign key is checked at the end of the statement, when both are gone.
_STEPS_OF_BATCH = """
WITH logs AS (
    DELETE FROM retrieval_logs
     WHERE pipeline_step_id IN (SELECT id FROM pipeline_steps WHERE message_batch_id = %(message_batch_id)s)
)
DELETE FROM pipeline_steps WHERE message_batch_id = %(message_batch_id)s
"""

_LOG = """
INSERT INTO retrieval_logs (id, pipeline_step_id, document_chunk_id, similarity_score, rank)
VALUES (%(id)s, %(pipeline_step_id)s, %(document_chunk_id)s, %(similarity_score)s, %(rank)s)
"""

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
    # One statement per row and not executemany: they are eleven at most, and the order of the rows is the order of the turn.
    for step in steps:
        step_id = ids.new()
        await conn.execute(
            _STEP,
            {
                "id": step_id,
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
        # top_k at most, three today: one statement each, in the order of their rank.
        for chunk in step.retrieved:
            await conn.execute(
                _LOG,
                {
                    "id": ids.new(),
                    "pipeline_step_id": step_id,
                    "document_chunk_id": chunk.document_chunk_id,
                    "similarity_score": chunk.similarity,
                    "rank": chunk.rank,
                },
            )
