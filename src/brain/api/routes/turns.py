# The one synchronous hop of the turn (ADR-0017 (e)): the core calls, the brain answers, and no queue sits between them.

import logging

from fastapi import APIRouter, HTTPException, Request, status

from adapters.llm.base import LLMError, LLMMessage
from adapters.llm.registry import resolve
from api.schemas.turn import TurnRequest, TurnResponse
from infra.enums import MessageDirection
from pipeline.repository import Turn, TurnNotFound, read_turn
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
        completion = await client.complete(
            model=turn.agent.llm_model,
            system=prompt.system,
            messages=_messages(turn, prompt.user),
            temperature=turn.agent.temperature,
            max_tokens=turn.agent.max_tokens,
        )
    except LLMError as err:
        # failed and a 200: an unusable provider does not improve on a retry, so the core must not be told to try again.
        logger.error("turn not generated", extra={"context": {**context, "error": str(err)}})
        return TurnResponse(outcome="failed")

    logger.info(
        "turn generated",
        extra={
            "context": {
                **context,
                "prompt_tokens": completion.prompt_tokens,
                "completion_tokens": completion.completion_tokens,
            }
        },
    )
    # generation_id stays absent until the row exists: it is the id of evidence nobody wrote yet.
    return TurnResponse(outcome="generated", completion_text=completion.text)


# The history travels as real user/assistant turns and the batch as one final user message, never flattened into one
# string: that is the shape Regla 1 declares, and it is what lets the model read the conversation as a conversation.
def _messages(turn: Turn, envelope: str) -> tuple[LLMMessage, ...]:
    # The operator's replies reach the model as the assistant's own: from the contact's side there is one voice answering.
    history = tuple(
        LLMMessage(
            role="user" if m.direction == MessageDirection.INBOUND else "assistant",
            text=m.content_text,
        )
        for m in turn.history
    )
    return (*history, LLMMessage(role="user", text=envelope))


# A batch is one turn, so its messages become one question: the customer wrote three lines, not three questions (F10).
def _incoming(turn: Turn) -> str:
    return "\n".join(m.content_text for m in turn.incoming)
