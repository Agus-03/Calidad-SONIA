# The one synchronous hop of the turn (ADR-0017 (e)): the core calls, the brain answers, and no queue sits between them.

import logging

from fastapi import APIRouter, HTTPException, Request, status

from api.schemas.turn import TurnRequest, TurnResponse
from pipeline.repository import TurnNotFound, read_turn

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

    logger.info(
        "turn read",
        extra={
            "context": {
                "conversation_id": str(turn.conversation_id),
                "turn_number": turn.turn_number,
                "incoming_messages": len(turn.incoming),
                "history_messages": len(turn.history),
                "llm_model": turn.agent.llm_model,
            }
        },
    )

    # failed and not blocked: nothing rejected this turn, there is simply no generator wired yet (that is the LLM adapter task).
    return TurnResponse(outcome="failed")
