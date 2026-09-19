# The read of layer 1 (03-capa-prompts 1): prompt_components belongs to the core, so the brain only reads it (regla 3).

from dataclasses import dataclass
from datetime import datetime
from uuid import UUID

from psycopg import AsyncConnection

from infra.enums import PromptComponentType

# Ordered by position because that order is the administrator's (Regla 1); active is F01's ablation switch and deleted_at is C09.
# The newest version travels with the text so the generation can name what it assembled (OE1). The core writes both in one
# transaction (ADR-0039, regla 3), so this one statement never sees a text without its version.
_COMPONENTS = """
SELECT c.type, c.content, c.position, v.id AS version_id, statement_timestamp() AS read_at
FROM   prompt_components c
JOIN   LATERAL (
           SELECT pv.id
           FROM   prompt_component_versions pv
           WHERE  pv.prompt_component_id = c.id
           ORDER  BY pv.version_number DESC
           LIMIT  1
       ) v ON true
WHERE  c.ai_agent_id = %(agent)s AND c.active AND c.deleted_at IS NULL
ORDER  BY c.position
"""


@dataclass(frozen=True)
class Component:
    type: PromptComponentType
    content: str
    position: int
    # The prompt_component_versions row this text is, which is what generation_components points at. None only for a draft of the
    # preview, which has no version yet and is never written (ADR-0040).
    version_id: UUID | None = None


# The name only: the preview gets the texts from the panel, and the name is the one thing of the agent the scaffolding prints.
# A paused agent answers too, because paused is when the administrator corrects it (ADR-0039, regla 8).
_AGENT_NAME = """
SELECT a.name
FROM   ai_agents a
WHERE  a.id = %(agent)s AND a.deleted_at IS NULL
"""


async def read_agent_name(conn: AsyncConnection, ai_agent_id: UUID) -> str | None:
    cursor = await conn.execute(_AGENT_NAME, {"agent": ai_agent_id})
    row = await cursor.fetchone()
    return row["name"] if row else None


# What was read and when, by the database's clock (C06): OE1 asks which versions were current at the instant of this read, not at
# the instant the generation is written seconds later (ADR-0039, regla 10).
@dataclass(frozen=True)
class Assembly:
    components: tuple[Component, ...]
    read_at: datetime


# Returns however many there are: F01 declares six, but a company with no brand voice is a valid state and not an error.
async def read_components(conn: AsyncConnection, ai_agent_id: UUID) -> Assembly:
    cursor = await conn.execute(_COMPONENTS, {"agent": ai_agent_id})
    rows = await cursor.fetchall()
    if rows:
        # The instant of the statement that read them, the closest the clock gets to the snapshot the texts came from.
        read_at = rows[0]["read_at"]
    else:
        cursor = await conn.execute("SELECT statement_timestamp() AS read_at")
        read_at = (await cursor.fetchone())["read_at"]
    return Assembly(
        components=tuple(
            Component(
                type=PromptComponentType(row["type"]),
                content=row["content"],
                position=row["position"],
                version_id=row["version_id"],
            )
            for row in rows
        ),
        read_at=read_at,
    )
