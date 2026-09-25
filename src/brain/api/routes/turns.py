# The one synchronous hop of the turn (ADR-0017 (e)): the core calls, the brain answers, and no queue sits between them.

import logging
from collections.abc import Sequence
from dataclasses import replace

from fastapi import APIRouter, HTTPException, Request, status

from adapters.llm.base import LLMClient, LLMError
from adapters.llm.registry import resolve
from api.schemas.turn import TurnRequest, TurnResponse
from classifiers.intent import Intent, classify
from infra.enums import GenerationStatus, PromptComponentType
from pipeline import runner
from pipeline.generation import UnpricedModel, generate
from pipeline.repository import (
    TracedStep,
    Turn,
    TurnNotFound,
    read_turn,
    write_generation,
    write_intents,
    write_steps,
)
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

        read = await read_components(conn, body.ai_agent_id)

    client = resolve(turn.agent.llm_provider, request.app.state.settings)
    message = _incoming(turn)

    # The steps of ADR-0016 that decide about what the customer wrote, before anything is generated. Its trace is written even
    # when it ends the turn: a batch the pipeline cut has these rows and no generation at all, which is the case the table
    # exists to be able to show.
    try:
        trace = await runner.run(
            steps=runner.INBOUND,
            turn=turn,
            client=client,
            text=message,
            components=read.components,
        )
    except runner.StepFailed as err:
        # The partial evidence is kept, which is what 07-secuencia section 5 asks of a turn that fell over halfway.
        logger.error(
            "turn not decided",
            extra={"context": {"conversation_id": str(turn.conversation_id), "error": str(err)}},
        )
        await _write_trace(request, turn, err.trace.steps)
        return TurnResponse(outcome="failed")

    if trace.halted:
        # No generation and no message: the core writes processed_at and drops the conversation to waiting (section 4.1).
        await _write_trace(request, turn, trace.steps)
        return TurnResponse(outcome="blocked")

    # What reaches the model from here on is what the leg returned and not what arrived: the redaction of ADR-0006 happens in
    # a step, and generations.prompt_rendered has to carry the redacted text and never the original.
    message = trace.text

    # First half of F11: how many different things the customer asked for in this batch. Before the assembly, because the
    # second half is handing that list to the model that answers (ADR-0041, regla 12).
    intents = await _intents(client, turn, message)

    # Assembled outside the connection: it is text handling and holding a pool connection through it starves the next turn.
    prompt = assemble(
        agent_name=turn.agent.name,
        components=read.components,
        message=message,
        intents=[i.label for i in intents],
    )
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
        "detected_intents": len(intents),
        "pipeline_steps": len(trace.steps),
    }

    try:
        generation = await generate(client=client, turn=turn, prompt=prompt, assembled_at=read.read_at)
    except (LLMError, UnpricedModel) as err:
        # failed and a 200: neither an unusable provider nor an unpriced model improves on a retry, so the core must not try again.
        logger.error("turn not generated", extra={"context": {**context, "error": str(err)}})
        return TurnResponse(outcome="failed")

    # The steps that decide about the answer: the same blocklist and the same guardrail, about a text that did not exist when the
    # leg above ran. It continues the numbering, so the trace of the turn reads in order across the two legs (ADR-0016).
    try:
        answer = await runner.run(
            steps=runner.OUTBOUND,
            turn=turn,
            client=client,
            text=generation.completion_text,
            start_position=trace.next_position,
            components=read.components,
        )
    except runner.StepFailed as err:
        # A declared cost and not an oversight (section 2): generations is written once and already with its verdict, so a brain
        # that falls over mid-guardrail loses that generation and leaves only the steps of the turn.
        logger.error("answer not decided", extra={"context": {**context, "error": str(err)}})
        await _write_trace(request, turn, trace.steps + err.trace.steps)
        return TurnResponse(outcome="failed")

    if answer.halted:
        # Written once with its verdict, and its text is kept: the evidence of section 4.2 exists so somebody can see what was
        # stopped and with what prompt it was produced. message_id stays NULL for ever, and the core never links it.
        generation = replace(generation, status=GenerationStatus.BLOCKED)

    steps = trace.steps + answer.steps
    context["pipeline_steps"] = len(steps)

    # A new connection and not the first one held open: the calls above take seconds and the pool is shared.
    async with request.app.state.database.connection() as conn:
        # First, because it is the first thing that happened: the trace reads in the order of the turn (ADR-0016).
        await write_steps(conn, turn.message_batch_id, steps)
        # Before the generation and in the same transaction: the evidence of F11 belongs to the turn that used it, and a
        # generation whose intents were lost would read as a turn that answered a list nobody can see.
        await write_intents(conn, turn.message_batch_id, intents)
        await write_generation(conn, generation)

    if answer.halted:
        logger.warning(
            "answer blocked after generating",
            extra={"context": {**context, "generation_id": str(generation.id)}},
        )
        # With the generation_id and without the text: the evidence has to be pointable at, and an answer that was refused must
        # not be sendable by the caller (section 4.2).
        return TurnResponse(outcome="blocked", generation_id=generation.id)

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


# On its own connection, because the legs may have spent seconds against a provider and the pool is shared. It is the only
# evidence a cut turn leaves, so it is not folded into the transaction of a generation that is never going to exist.
async def _write_trace(request: Request, turn: Turn, steps: Sequence[TracedStep]) -> None:
    async with request.app.state.database.connection() as conn:
        await write_steps(conn, turn.message_batch_id, steps)


# A batch is one turn, so its messages become one question: the customer wrote three lines, not three questions (F10).
def _incoming(turn: Turn) -> str:
    return "\n".join(m.content_text for m in turn.incoming)


# Never lets the classifier end the turn: the customer is owed an answer, and a batch whose intents were not read is answered
# the way every batch was answered until F11 existed. The gap is logged, which is what makes it countable later.
async def _intents(client: LLMClient, turn: Turn, message: str) -> tuple[Intent, ...]:
    try:
        return await classify(client=client, model=turn.agent.llm_model, message=message)
    except LLMError as err:
        logger.warning(
            "intents not classified",
            extra={
                "context": {
                    "conversation_id": str(turn.conversation_id),
                    "message_batch_id": str(turn.message_batch_id),
                    "error": str(err),
                }
            },
        )
        return ()
