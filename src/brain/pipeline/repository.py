# The turn's reads, next to what uses them: the brain gets four ids and finds everything else here (07-secuencia 1).

import logging
from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal
from uuid import UUID

from psycopg import AsyncConnection

from infra.enums import GenerationStatus, MessageDirection, SenderType

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


# message_id is absent on purpose: the core links it inside the transaction of section 2 (ADR-0017 (i).2).
_GENERATION = """
INSERT INTO generations (
    id, conversation_id, ai_agent_id, message_batch_id,
    llm_provider, llm_model, prompt_rendered, completion_text,
    prompt_tokens, completion_tokens, latency_ms, cost, currency, status
) VALUES (
    %(id)s, %(conversation_id)s, %(ai_agent_id)s, %(message_batch_id)s,
    %(llm_provider)s, %(llm_model)s, %(prompt_rendered)s, %(completion_text)s,
    %(prompt_tokens)s, %(completion_tokens)s, %(latency_ms)s, %(cost)s, %(currency)s, %(status)s
)
"""


# Takes the connection like every read here, so the caller keeps owning the transaction (regla 5 on the Python side).
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
        },
    )
