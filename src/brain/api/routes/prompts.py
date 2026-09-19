# The preview of F03: the same assemble() the turn runs, over the drafts on screen, so the administrator reads what the model reads.

from fastapi import APIRouter, HTTPException, Request, status

from api.schemas.preview import PreviewRequest, PreviewResponse
from prompts.assembler import assemble
from prompts.repository import Component, read_agent_name

router = APIRouter(tags=["prompts"])


# Writes nothing: a preview is not evidence, so it has no row among the tables the brain may write (regla 3).
@router.post("/prompts/preview", response_model=PreviewResponse)
async def preview(request: Request, body: PreviewRequest) -> PreviewResponse:
    async with request.app.state.database.connection() as conn:
        name = await read_agent_name(conn, body.ai_agent_id)
    if name is None:
        # A 404 and not a 500: the core checked the agent before asking, so it was removed in between.
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail={"code": "ai_agent_not_found", "message": f"no ai agent {body.ai_agent_id}"},
        )

    # Sorted here because the turn gets them sorted by its query: the order of the prompt is position, not the order of the body.
    drafts = sorted(body.components, key=lambda c: c.position)
    # No version: a draft has none yet, and generate() refuses a component without one, so a draft never becomes evidence.
    prompt = assemble(
        agent_name=name,
        components=[Component(type=c.type, content=c.content, position=c.position) for c in drafts],
        # The envelope is assembled and discarded: only the system is previewed (ADR-0040).
        message="",
    )
    return PreviewResponse(
        system=prompt.system,
        rules_version=prompt.rules_version,
        components=[c.type for c in prompt.assembled],
    )
