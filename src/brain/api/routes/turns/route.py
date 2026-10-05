# The one synchronous hop of the turn (ADR-0017 (e)): the core calls, the brain answers, and no queue sits between them.

import asyncio
import logging
from collections.abc import Sequence
from dataclasses import replace
from time import perf_counter

from fastapi import APIRouter, HTTPException, Request, status

from adapters.llm.base import LLMError
from adapters.llm.registry import resolve
from api.routes.turns import enrichment, signals
from api.schemas.turn import ExtractedDatum, TransferProposal, TurnRequest, TurnResponse
from classifiers import extraction, transfer
from infra.enums import (
    AgentMode,
    GenerationStatus,
    MessageDirection,
    PipelineStep,
    PromptComponentType,
    TransferSignalType,
)
from knowledge.search import SearchRules
from pipeline import grounding, notices, retrieval, runner, triage
from pipeline.generation import UnpricedModel, generate
from pipeline.repository.turn import Turn, TurnNotFound, read_turn
from pipeline.repository.context import read_context, write_context_version
from pipeline.repository.signals import read_transfer_policies, write_signal_evaluation
from pipeline.repository.evidence import TracedStep, write_generation, write_intents, write_steps
from pipeline.step import Step
from prompts import rewriter
from prompts.assembler import assemble, rules
from prompts.repository import read_components, read_quick_replies

router = APIRouter(tags=["turns"])
logger = logging.getLogger("sonia.api.turns")

# The outcome domain declared for the generation step in DER/entidades.md: it produced an answer or it did not. Written here and
# not in a module of pipeline/, because this step has no module: what generates is generation.py and what decides is nothing.
_OK = "ok"
_FAILED = "failed"


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
        # Read with the components, on every turn and not cached: a reply the admin paused leaves the next prompt (F07, ADR-0061).
        replies = await read_quick_replies(conn, body.ai_agent_id, rules().quick_replies_limit)

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

    if trace.halted and trace.halted_at.step != PipelineStep.TRIAGE:
        # A cut before the triage evaluates no signal: that would send the provider the text the cut refused (ADR-0045, 2.6).
        trace = signals.annotated(trace, skipped=transfer.ORDER)
        await _write_trace(request, turn, trace.steps)
        return TurnResponse(outcome="blocked", notice_text=_notice(trace.halted_at))

    # The checkpoint half of F31, on every turn that passed the inbound leg: a request for a person often comes with the DNI, and
    # these three never reach a model (ADR-0047, sections 2.3 and 2.7).
    found = extraction.sensitive(turn.incoming) if turn.agent.extraction_enabled else ()

    # The three signals of this call, written whether they fired or not, before anything else can fail (ADR-0014, ADR-0045).
    async with request.app.state.database.connection() as conn:
        assessment = await signals.assess(conn, turn, trace)
        for evaluation in assessment.evaluations:
            await write_signal_evaluation(conn, evaluation)
    trace = signals.annotated(trace, mode=body.mode, assessment=assessment)

    if assessment.winner is not None and not trace.halted:
        # Delegates like out_of_domain does: no generation and nothing said to the customer (ADR-0045, section 2.5; O1c).
        trace = replace(trace, halted_at=_TRIAGE)

    if trace.halted:
        # No generation, and the core drops the conversation to waiting (section 4 B.1) and acts on the proposal.
        await _write_trace(request, turn, trace.steps)
        return TurnResponse(
            outcome="blocked",
            notice_text=_notice(trace.halted_at),
            transfer=signals.proposal(assessment),
            extracted=_extracted(found),
        )

    # What reaches the model from here on is what the leg returned and not what arrived: the redaction of ADR-0006 happens in
    # a step, and generations.prompt_rendered has to carry the redacted text and never the original.
    message = trace.text

    # Read before the retrieval, which searches with its summary (ADR-0068, regla 2). Only a turn that reaches generation
    # rewrites it: a cut one has nothing to give it to (ADR-0048).
    async with request.app.state.database.connection() as conn:
        current = await read_context(conn, turn.conversation_id)

    # The fourth and fifth steps of the turn, after the signals and not inside the inbound leg: a turn the triage already
    # delegated does not pay a search it will never use, and the trace still reads triage, retrieval, grounding (ADR-0074).
    knowledge: tuple[str, ...] = ()
    searcher = request.app.state.searcher
    if searcher is not None and not _greeting(trace):
        summary = current.context.summary if current is not None else None
        retrieved = await retrieval.run(
            searcher, agent_id=turn.agent.id, summary=summary, block=message, position=trace.next_position
        )
        if retrieved is not None and retrieved.failed:
            # The step fails, as the triage does when its provider fails (ADR-0074, regla 2): answering without the documents
            # could invent, and transferring would count against OE6 what is a failure of ours. Its row is written with the steps
            # that decided, so OE6 reads the block as not answered (ADR-0077).
            logger.error(
                "turn not retrieved",
                extra={"context": {"conversation_id": str(turn.conversation_id), "error": retrieved.error}},
            )
            await _write_trace(request, turn, trace.steps + (retrieved.row,))
            return TurnResponse(outcome="failed")
        if retrieved is not None:
            grounded = grounding.run(retrieved, rules=searcher.rules, position=trace.next_position + 1)
            trace = replace(
                trace, steps=trace.steps + (retrieved.row, grounded.row), next_position=trace.next_position + 2
            )
            if not grounded.grounded:
                return await _not_grounded(request, body, turn, trace, grounded, searcher.rules, found)
            knowledge = grounded.knowledge

    # First half of F11, and the rewrite of F04 beside it: neither reads the other, and the customer waits for the slower one and
    # not for both (ADR-0048). Before the assembly, because the answer is given the two (ADR-0041, regla 12; Regla 1).
    intents, rewritten, said = await asyncio.gather(
        enrichment.intents(client, turn, message),
        enrichment.rewrite(client, turn, message, current),
        # The other half of F31, over the redacted text and only where it gets an answer: a cut turn does not pay a call more.
        enrichment.extract(client, turn, message),
    )
    extracted = _extracted(found + said)
    dynamic, version, version_id = enrichment.dynamic(turn, current, rewritten, intents)

    # Assembled outside the connection: it is text handling and holding a pool connection through it starves the next turn.
    prompt = assemble(
        agent_name=turn.agent.name,
        components=read.components,
        message=message,
        context=rewriter.render(dynamic) if dynamic is not None else None,
        knowledge=knowledge,
        quick_replies=replies,
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
        "quick_replies": len(replies),
        # How many chunks of the knowledge base went in, and never their text: it is in retrieval_logs (ADR-0068, regla 6).
        "knowledge_chunks": len(knowledge),
        "context_rewritten": version is not None,
        # How many and never which: the values are data of the contact, and a log line is not where they get copied to (ADR-0006).
        "extracted": len(extracted),
        "pipeline_steps": len(trace.steps),
    }

    # Measured here and not inside generate(), for the one case where there is no generation to ask: a step that fell over still
    # owes the trace a latency, and the row below is the only place this turn will ever say how long it waited.
    started = perf_counter()
    try:
        generation = await generate(client=client, turn=turn, prompt=prompt, assembled_at=read.read_at)
    except (LLMError, UnpricedModel) as err:
        # failed and a 200: neither an unusable provider nor an unpriced model improves on a retry, so the core must not try again.
        logger.error("turn not generated", extra={"context": {**context, "error": str(err)}})
        # And the steps that did decide are kept, which section 5 asks of every turn that fell over halfway: this was the one place
        # that dropped them, so a batch whose generation never happened read as a batch on which nothing had ever run. The row of
        # the generation goes with them, and it is the only evidence this turn leaves: generations has no row at all.
        failed = _generated(
            position=trace.next_position,
            outcome=_FAILED,
            version=prompt.rules_version,
            latency_ms=round((perf_counter() - started) * 1000),
            # The class of the error and never its message: a provider's text is its own, and this column is read by a person who
            # needs to tell a timeout from an unpriced model, which is exactly what the name says.
            detail={"error": type(err).__name__},
        )
        await _write_trace(request, turn, trace.steps + (failed,))
        return TurnResponse(outcome="failed")
    generation = replace(generation, context_version_id=version_id)

    # The sixth step of ADR-0016, and the only row of the trace whose latency is not measured by whoever writes it: generate()
    # timed its own call, and using that number keeps the trace and generations from disagreeing about the same provider call.
    generated = _generated(
        position=trace.next_position,
        outcome=_OK,
        version=prompt.rules_version,
        latency_ms=generation.latency_ms,
        # The pointer and nothing else: the tokens, the cost and the text of this answer are in generations, and the DER already
        # takes the cost of a row that mostly points at another table.
        detail={"generation_id": str(generation.id)},
    )

    # The steps that decide about the answer: the same blocklist and the same guardrail, about a text that did not exist when the
    # leg above ran. It continues the numbering, so the trace of the turn reads in order across the two legs (ADR-0016).
    try:
        answer = await runner.run(
            steps=runner.OUTBOUND,
            turn=turn,
            client=client,
            text=generation.completion_text,
            # One further along than the leg above ended, because the generation took that position: position is the order things
            # happened in across the whole turn and not inside a leg (ADR-0016).
            start_position=trace.next_position + 1,
            components=read.components,
        )
    except runner.StepFailed as err:
        # A declared cost and not an oversight (section 2): generations is written once and already with its verdict, so a brain
        # that falls over mid-guardrail loses that generation and leaves only the steps of the turn.
        logger.error("answer not decided", extra={"context": {**context, "error": str(err)}})
        await _write_trace(request, turn, trace.steps + (generated,) + err.trace.steps)
        return TurnResponse(outcome="failed")

    if answer.halted:
        # Written once with its verdict, and its text is kept: the evidence of section 4.2 exists so somebody can see what was
        # stopped and with what prompt it was produced. message_id stays NULL for ever, and the core never links it.
        generation = replace(generation, status=GenerationStatus.BLOCKED)
    elif body.mode == AgentMode.SUGGESTION:
        # A draft for the Operator and never an answer: nothing leaves on its own in this mode (ADR-0046, section 2.2).
        generation = replace(generation, status=GenerationStatus.PENDING_REVIEW)

    steps = trace.steps + (generated,) + answer.steps
    context["pipeline_steps"] = len(steps)

    # A new connection and not the first one held open: the calls above take seconds and the pool is shared.
    async with request.app.state.database.connection() as conn:
        # First, because it is the first thing that happened: the trace reads in the order of the turn (ADR-0016).
        await write_steps(conn, turn.message_batch_id, steps)
        # Before the generation and in the same transaction: the evidence of F11 belongs to the turn that used it, and a
        # generation whose intents were lost would read as a turn that answered a list nobody can see.
        await write_intents(conn, turn.message_batch_id, intents)
        # Before the generation, which points at it; the core copies it into conversation_contexts when it closes the turn.
        if version is not None:
            await write_context_version(conn, version)
        await write_generation(conn, generation)

    if answer.halted:
        logger.warning(
            "answer blocked after generating",
            extra={"context": {**context, "generation_id": str(generation.id)}},
        )
        # With the generation_id and without the text: the evidence has to be pointable at, and an answer that was refused must
        # not be sendable by the caller (section 4.2). What travels instead is the fixed text of this cut point (O2), which is a
        # different string in a different field for exactly that reason.
        return TurnResponse(
            outcome="blocked",
            generation_id=generation.id,
            notice_text=_notice(answer.halted_at),
            extracted=extracted,
        )

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
        extracted=extracted,
    )


# The row of the sixth step of ADR-0016, built here and not by the runner: the runner walks one leg, and the generation is what
# happens between the two. Without it the trace jumps from position 3 to 4 over the one thing in the turn that cost money, and a
# turn that died generating says nothing about where it died.
def _generated(*, position: int, outcome: str, version: int, latency_ms: int, detail: dict) -> TracedStep:
    return TracedStep(
        step=PipelineStep.GENERATION,
        # outbound, because what this step produced is the answer: it is the same reading direction has on the second run of the
        # blocklist and the guardrail, which decide about that answer right after this row.
        direction=MessageDirection.OUTBOUND,
        position=position,
        outcome=outcome,
        # The version of the rules the prompt was assembled with (decision D5, regla 9): what decided how this turn was asked is
        # the assembly, and the model that answered is already a column of generations.
        version=str(version),
        latency_ms=latency_ms,
        # No score: this step produces no confidence, and the two that do are triage and grounding.
        detail=detail,
    )


# The step the transfer cuts at, looked up and not rebuilt: the inbound leg is data (ADR-0016).
_TRIAGE = next(s for s in runner.INBOUND if s.step == PipelineStep.TRIAGE)


# ADR-0068, regla 9: a greeting has nothing to search, and would come out not_grounded and land on a seller's tray.
def _greeting(trace: runner.Trace) -> bool:
    row = next((s for s in trace.steps if s.step == PipelineStep.TRIAGE), None)
    verdict = triage.verdict_of(row) if row is not None else None
    return verdict is not None and verdict.greeting


# The cut of the grounding (ADR-0068, reglas 7 and 8): the out_of_domain signal under the retrieval's own classifier_version, the
# fixed text, and a transfer when the agent's policy for that signal is on. The trace goes with it, retrieval_logs included,
# which is the evidence of the cut. In suggestion mode the core sends no text and transfers nothing (ADR-0046).
async def _not_grounded(
    request: Request,
    body: TurnRequest,
    turn: Turn,
    trace: runner.Trace,
    grounded: grounding.Grounding,
    rules: SearchRules,
    found: Sequence[extraction.Extracted],
) -> TurnResponse:
    async with request.app.state.database.connection() as conn:
        policies = await read_transfer_policies(conn, turn.agent.id)
        evaluation = grounding.signal(
            grounded,
            message_batch_id=turn.message_batch_id,
            rules=rules,
            policy=policies.get(TransferSignalType.OUT_OF_DOMAIN),
        )
        await write_signal_evaluation(conn, evaluation)

    # The mode and the transfer on the row that cut, like the triage's row carries them (ADR-0045 2.5, ADR-0046 2.4).
    cut = trace.steps[-1]
    detail = {**(cut.detail or {}), "mode": str(body.mode)}
    if evaluation.fired:
        detail["transfer"] = str(TransferSignalType.OUT_OF_DOMAIN)
    trace = replace(trace, steps=trace.steps[:-1] + (replace(cut, detail=detail),))
    await _write_trace(request, turn, trace.steps)

    logger.info(
        "the grounding sent the turn to a person",
        extra={
            "context": {
                "conversation_id": str(turn.conversation_id),
                "best_similarity": str(grounded.best) if grounded.best is not None else None,
                "transfer": evaluation.fired,
            }
        },
    )
    return TurnResponse(
        outcome="blocked",
        notice_text=notices.rules().text_for(PipelineStep.GROUNDING, MessageDirection.INBOUND),
        transfer=TransferProposal(proposed=evaluation.fired, signal_evaluation_id=evaluation.id),
        extracted=_extracted(found),
    )


# What the customer reads when the pipeline refused the turn (O2), or nothing at all when the step that cut was the triage (O1c).
# Takes the step and not the trace because a leg that did not halt has no cut point, and asking for its text would be a bug here
# rather than a missing entry in the file.
def _notice(halted_at: Step | None) -> str | None:
    if halted_at is None:
        return None
    return notices.rules().text_for(halted_at.step, halted_at.direction)


# On its own connection, because the legs may have spent seconds against a provider and the pool is shared. It is the only
# evidence a cut turn leaves, so it is not folded into the transaction of a generation that is never going to exist.
async def _write_trace(request: Request, turn: Turn, steps: Sequence[TracedStep]) -> None:
    async with request.app.state.database.connection() as conn:
        await write_steps(conn, turn.message_batch_id, steps)


# A batch is one turn, so its messages become one question: the customer wrote three lines, not three questions (F10).
def _incoming(turn: Turn) -> str:
    return "\n".join(m.content_text for m in turn.incoming)


# The contract's shape of what was found. float, because it is a confidence and not money (C08).
def _extracted(found: Sequence[extraction.Extracted]) -> list[ExtractedDatum]:
    return [
        ExtractedDatum(
            key=e.key, value=e.value, confidence=float(e.confidence), source_message_id=e.source_message_id
        )
        for e in found
    ]
