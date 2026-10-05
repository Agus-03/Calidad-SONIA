# The signals of F12 around the inbound leg: what the turn reads to judge them, what it proposes and what the trace says (ADR-0045).

from dataclasses import replace

from psycopg import AsyncConnection

from api.schemas.turn import TransferProposal
from classifiers import transfer
from infra.enums import AgentMode, PipelineStep, TransferSignalType
from pipeline import runner, triage
from pipeline.repository.turn import Turn
from pipeline.repository.signals import read_no_resolution_streak, read_transfer_policies


# The reads the signals need, on the caller's connection: the policies of the AIAgent and, only when this turn did not resolve,
# the run it continues (ADR-0045, section 2.2).
async def assess(conn: AsyncConnection, turn: Turn, trace: runner.Trace) -> transfer.Assessment:
    row = next((s for s in trace.steps if s.step == PipelineStep.TRIAGE), None)
    verdict = triage.verdict_of(row) if row is not None else None
    version = row.version if row is not None else ""
    policies = await read_transfer_policies(conn, turn.agent.id) if verdict is not None else {}
    streak = 0
    if verdict is not None and verdict.resolved is False:
        streak = await read_no_resolution_streak(conn, turn, version)
    return transfer.assess(
        message_batch_id=turn.message_batch_id,
        verdict=verdict,
        classifier_version=version,
        policies=policies,
        defaults=triage.rules().thresholds,
        previous_streak=streak,
    )


# The winner when one fired; otherwise, on a cut of out_of_domain, that evidence with proposed = false, because the core acts on
# the flag and the id still names what was evaluated (decision O1c).
def proposal(assessment: transfer.Assessment) -> TransferProposal | None:
    if assessment.winner is not None:
        return TransferProposal(proposed=True, signal_evaluation_id=assessment.winner.id)
    evaluated = assessment.of(TransferSignalType.OUT_OF_DOMAIN)
    if evaluated is None:
        return None
    return TransferProposal(proposed=False, signal_evaluation_id=evaluated.id)


# What the row of the triage (or of the step that cut before it) says about the signals: which were skipped, which one cut, and the
# mode, which is how OE6 leaves suggestion out of its denominator (ADR-0044 2.3, ADR-0045 2.5, ADR-0046 2.4).
def annotated(
    trace: runner.Trace,
    *,
    mode: AgentMode | None = None,
    assessment: transfer.Assessment | None = None,
    skipped: tuple[TransferSignalType, ...] = (),
) -> runner.Trace:
    if assessment is not None:
        index = next((i for i, s in enumerate(trace.steps) if s.step == PipelineStep.TRIAGE), None)
        skipped = assessment.skipped
    else:
        index = len(trace.steps) - 1
    if index is None or index < 0:
        return trace

    row = trace.steps[index]
    detail = dict(row.detail or {})
    detail["skipped_signals"] = [str(signal) for signal in skipped]
    if mode is not None:
        detail["mode"] = str(mode)
    if assessment is not None and assessment.winner is not None:
        detail["transfer"] = str(assessment.winner.signal_type)
    steps = trace.steps[:index] + (replace(row, detail=detail),) + trace.steps[index + 1 :]
    return replace(trace, steps=steps)
