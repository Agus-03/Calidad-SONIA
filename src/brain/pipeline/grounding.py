# The grounding step of ADR-0016, inbound: whether what the retrieval found backs an answer at all, decided by a threshold on the
# similarity and not by a model, so it is free, adds no latency and gives the same verdict for the same search (ADR-0068, regla 4).
# The outbound grounding, against a hallucinated answer, is built in the evaluation phase (ADR-0068, regla 11).
#
# not_grounded ends the turn and writes the out_of_domain signal of F12, because a question the documents do not answer is the
# letter of "consultas fuera del dominio RAG" (ADR-0068, regla 7).

from dataclasses import dataclass
from decimal import Decimal
from time import perf_counter
from uuid import UUID

from infra import ids
from infra.enums import MessageDirection, PipelineStep, TransferSignalType
from knowledge.search import SearchRules
from pipeline.repository.evidence import TracedStep
from pipeline.repository.signals import SignalEvaluation, TransferPolicy
from pipeline.retrieval import Retrieval, similarity

# The outcome domain declared for this step in DER/entidades.md.
GROUNDED = "grounded"
NOT_GROUNDED = "not_grounded"

_ONE = Decimal(1)


@dataclass(frozen=True)
class Grounding:
    row: TracedStep
    grounded: bool
    # The best similarity the search found, None when it found nothing: the score of the row and what the signal is made of.
    best: Decimal | None
    # The text of the chunks that reach the threshold, in rank order: only these reach the generation (ADR-0068, regla 4).
    knowledge: tuple[str, ...]


def run(retrieval: Retrieval, *, rules: SearchRules, position: int) -> Grounding:
    started = perf_counter()
    best = max((similarity(f) for f in retrieval.found), default=None)
    passed = tuple(f.content for f in retrieval.found if similarity(f) >= rules.threshold)
    grounded = best is not None and best >= rules.threshold
    row = TracedStep(
        step=PipelineStep.GROUNDING,
        direction=MessageDirection.INBOUND,
        position=position,
        outcome=GROUNDED if grounded else NOT_GROUNDED,
        # The same file as the retrieval: the threshold lives in it (ADR-0068, regla 5).
        version=str(rules.version),
        latency_ms=round((perf_counter() - started) * 1000),
        # The best similarity, or nothing when nothing was found: there is no confidence to report about an empty search.
        score=best,
        detail={"threshold": str(rules.threshold), "passed": len(passed)},
    )
    return Grounding(row=row, grounded=grounded, best=best, knowledge=passed)


# The row of signal_evaluations a not_grounded writes (ADR-0068, regla 7). Turned around so it fires in the same direction as the
# other signals, higher meaning further from the documents: the score is 1 minus the best similarity, 1 when nothing was found,
# and the threshold 1 minus the threshold. retrieval<version> tells it from the row the triage wrote for the same signal.
# It fires when the policy of out_of_domain is on: the same policy as the triage's, because F12 names one signal.
def signal(grounding: Grounding, *, message_batch_id: UUID, rules: SearchRules, policy: TransferPolicy | None) -> SignalEvaluation:
    return SignalEvaluation(
        id=ids.new(),
        message_batch_id=message_batch_id,
        signal_type=TransferSignalType.OUT_OF_DOMAIN,
        score=_ONE - grounding.best if grounding.best is not None else _ONE,
        threshold_used=_ONE - rules.threshold,
        classifier_version=f"retrieval{rules.version}",
        # Not score against threshold: a best exactly at the threshold is grounded, and its turned-around score equals the
        # turned-around threshold. What decided is the verdict of this step, and the policy says whether it transfers.
        fired=not grounding.grounded and policy is not None and policy.enabled,
    )
