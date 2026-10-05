# What runs beside the generation and never ends the turn: the rewrite of F04, the intents of F11 and the extraction of F31.

import logging
from collections.abc import Sequence
from dataclasses import replace
from uuid import UUID

from adapters.llm.base import LLMClient, LLMError
from classifiers import extraction
from classifiers.intent import Intent, classify
from infra import ids
from pipeline.repository.turn import Turn
from pipeline.repository.context import Context, ContextVersion, CurrentContext
from prompts import rewriter

# The turn's logger and not one of its own: these lines are about the turn, and a reader filters them by that name.
logger = logging.getLogger("sonia.api.turns")


# Never lets the extraction end the turn: a datum not found is a datum the next batch may still bring, and the customer is owed the
# answer either way. Off by the toggle, it is never called (ADR-0047, section 2.4).
async def extract(client: LLMClient, turn: Turn, message: str) -> tuple[extraction.Extracted, ...]:
    if not turn.agent.extraction_enabled:
        return ()
    try:
        return await extraction.classify(
            client=client, model=turn.agent.llm_model, message=message, incoming=turn.incoming
        )
    except LLMError as err:
        logger.warning(
            "contact data not extracted",
            extra={
                "context": {
                    "conversation_id": str(turn.conversation_id),
                    "message_batch_id": str(turn.message_batch_id),
                    "error": str(err),
                }
            },
        )
        return ()


# Never lets the rewrite end the turn, and never asks twice: Regla 3 answers with the context there was, and the core waits ten
# seconds for all of it (ADR-0048). The gap is logged, and countable as a generation that points at an older turn's version.
async def rewrite(
    client: LLMClient, turn: Turn, message: str, current: CurrentContext | None
) -> rewriter.Rewrite | None:
    try:
        return await rewriter.rewrite(
            client=client,
            model=turn.agent.llm_model,
            previous=current.context if current is not None else None,
            message=message,
        )
    except LLMError as err:
        logger.warning(
            "context not rewritten",
            extra={
                "context": {
                    "conversation_id": str(turn.conversation_id),
                    "message_batch_id": str(turn.message_batch_id),
                    "error": str(err),
                }
            },
        )
        return None


# This turn's rewrite with the intent F11 read, and the version that records it; or, when it failed, the context there was and
# the version it came from, which is what the generation points at (03-capa-prompts, Regla 3).
def dynamic(
    turn: Turn,
    current: CurrentContext | None,
    rewritten: rewriter.Rewrite | None,
    intents: Sequence[Intent],
) -> tuple[Context | None, ContextVersion | None, UUID | None]:
    if rewritten is None:
        if current is None:
            return None, None, None
        return current.context, None, current.version_id

    context = replace(rewritten.context, primary_intent=rewriter.primary(intents) or rewritten.context.primary_intent)
    version = ContextVersion(
        id=ids.new(),
        conversation_id=turn.conversation_id,
        turn_number=turn.turn_number,
        context=context,
        rewritten_by_model=rewritten.model,
        latency_ms=rewritten.latency_ms,
    )
    return context, version, version.id


# Never lets the classifier end the turn: the customer is owed an answer, and a batch whose intents were not read is answered
# the way every batch was answered until F11 existed. The gap is logged, which is what makes it countable later.
async def intents(client: LLMClient, turn: Turn, message: str) -> tuple[Intent, ...]:
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
