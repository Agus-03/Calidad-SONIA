# The dynamic context of F04: what the turn starts from and the version each rewrite leaves (ADR-0048).

from collections.abc import Mapping
from dataclasses import dataclass
from types import MappingProxyType
from typing import Any
from uuid import UUID

from psycopg import AsyncConnection
from psycopg.types.json import Jsonb


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
