# The one synchronous hop of the turn (ADR-0017 (e)): the core calls, the brain answers, and no queue sits between them.

import logging

from fastapi import APIRouter, HTTPException, Request, status

from adapters.llm.base import LLMError
from adapters.llm.registry import resolve
from api.schemas.turn import TurnRequest, TurnResponse
from infra.enums import PromptComponentType
from pipeline.generation import UnpricedModel, generate
from pipeline.repository import Turn, TurnNotFound, read_turn, write_generation
from prompts.assembler import assemble
from prompts.repository import read_components

router = APIRouter(tags=["turns"])
logger = logging.getLogger("sonia.api.turns")


@router.post("/turns", response_model=TurnResponse)
async def create_turn(request: Request, body: TurnRequest) -> TurnResponse:
    async with request.app.state.database.connection() as conn:
        try:
            turn = await read_turn(
                conn, body.message_batch_id, body.conversation_id, body.ai_agent_id
            )
        except TurnNotFound as err:
            # A 404 and not a 500: four ids that name nothing is the caller's mistake, and retrying will not fix it.
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail={"code": "turn_not_found", "message": str(err)},
            ) from err

        components = await read_components(conn, body.ai_agent_id)

    # Assembled outside the connection: it is text handling and holding a pool connection through it starves the next turn.
    prompt = assemble(agent_name=turn.agent.name, components=components, message=_incoming(turn))
    if prompt.components < len(PromptComponentType):
        # Logged and not refused: F01 declares six, and an agent whose administrator has not written them all still answers.
        logger.info(
            "prompt assembled with fewer components than F01 declares",
            extra={
                "context": {
                    "conversation_id": str(turn.conversation_id),
                    "components": prompt.components,
                    "declared": len(PromptComponentType),
                }
            },
        )

    context = {
        "conversation_id": str(turn.conversation_id),
        "turn_number": turn.turn_number,
        "llm_provider": turn.agent.llm_provider,
        "llm_model": turn.agent.llm_model,
        "prompt_components": prompt.components,
        "prompt_rules_version": prompt.rules_version,
    }

    client = resolve(turn.agent.llm_provider, request.app.state.settings)
    try:
        generation = await generate(client=client, turn=turn, prompt=prompt)
    except (LLMError, UnpricedModel) as err:
        # failed and a 200: neither an unusable provider nor an unpriced model improves on a retry, so the core must not try again.
        logger.error("turn not generated", extra={"context": {**context, "error": str(err)}})
        return TurnResponse(outcome="failed")

    # A new connection and not the first one held open: the call above takes seconds and the pool is shared.
    async with request.app.state.database.connection() as conn:
        await write_generation(conn, generation)

    logger.info(
        "turn generated",
        extra={
            "context": {
                **context,
                "generation_id": str(generation.id),
                "prompt_tokens": generation.prompt_tokens,
                "completion_tokens": generation.completion_tokens,
                "latency_ms": generation.latency_ms,
                # As text, the same way the core reads it out of the row: money never travels as a float (C08).
                "cost": str(generation.cost),
                "currency": generation.currency,
            }
        },
    )
    return TurnResponse(
        outcome="generated",
        generation_id=generation.id,
        completion_text=generation.completion_text,
    )


# A batch is one turn, so its messages become one question: the customer wrote three lines, not three questions (F10).
def _incoming(turn: Turn) -> str:
    return "\n".join(m.content_text for m in turn.incoming)
