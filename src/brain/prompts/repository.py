# The read of layer 1 (03-capa-prompts 1): prompt_components belongs to the core, so the brain only reads it (regla 3).

from dataclasses import dataclass
from uuid import UUID

from psycopg import AsyncConnection

from infra.enums import PromptComponentType

# Ordered by position because that order is the administrator's (Regla 1); active is F01's ablation switch and deleted_at is C09.
# The newest version travels with the text so the generation can name what it assembled (OE1). The core writes both in one
# transaction (ADR-0039, regla 3), so this one statement never sees a text without its version.
_COMPONENTS = """
SELECT c.type, c.content, c.position, v.id AS version_id
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
    # The prompt_component_versions row this text is, which is what generation_components points at.
    version_id: UUID


# Returns however many there are: F01 declares six, but a company with no brand voice is a valid state and not an error.
async def read_components(conn: AsyncConnection, ai_agent_id: UUID) -> tuple[Component, ...]:
    cursor = await conn.execute(_COMPONENTS, {"agent": ai_agent_id})
    return tuple(
        Component(
            type=PromptComponentType(row["type"]),
            content=row["content"],
            position=row["position"],
            version_id=row["version_id"],
        )
        for row in await cursor.fetchall()
    )
