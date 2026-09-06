# The read of layer 1 (03-capa-prompts section 1): the six components the administrator edits, next to what assembles them.
# Reads and nothing else, because prompt_components belongs to the core and the brain only writes evidence (regla 3).

from dataclasses import dataclass
from uuid import UUID

from psycopg import AsyncConnection

from infra.enums import PromptComponentType

# Ordered by position because that is the order Regla 1 assembles them in, and it belongs to the administrator.
# active and deleted_at both filter: the first is F01's ablation switch and the second is the definitive removal (C09).
_COMPONENTS = """
SELECT c.type, c.content, c.position
FROM   prompt_components c
WHERE  c.ai_agent_id = %(agent)s AND c.active AND c.deleted_at IS NULL
ORDER  BY c.position
"""


@dataclass(frozen=True)
class Component:
    type: PromptComponentType
    content: str
    position: int


# Returns however many there are: F01 declares six, but a company with no brand voice is a valid state and not an error.
async def read_components(conn: AsyncConnection, ai_agent_id: UUID) -> tuple[Component, ...]:
    cursor = await conn.execute(_COMPONENTS, {"agent": ai_agent_id})
    return tuple(
        Component(
            type=PromptComponentType(row["type"]),
            content=row["content"],
            position=row["position"],
        )
        for row in await cursor.fetchall()
    )
