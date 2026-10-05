# The four signals of F12 turned into rows of signal_evaluations, and which one a transfer is attributed to (ADR-0045, ADR-0021).
# Pure and sync: the reads it needs are the route's, so every rule below is tested without a connection.

from collections.abc import Mapping
from dataclasses import dataclass
from decimal import Decimal
from uuid import UUID

from infra import ids
from infra.enums import TransferSignalType
from pipeline.repository.signals import SignalEvaluation, TransferPolicy
from pipeline.triage import Verdict

# The fixed order of ADR-0021: the first signal that fired is the one the transfer is attributed to.
ORDER = (
    TransferSignalType.EXPLICIT_REQUEST,
    TransferSignalType.FRUSTRATION,
    TransferSignalType.OUT_OF_DOMAIN,
    TransferSignalType.NO_RESOLUTION,
)

# A yes/no signal is 1 or 0 against 1, so threshold_used means one thing in every row (ADR-0045, section 2.4).
_YES = Decimal(1)
_NO = Decimal(0)


@dataclass(frozen=True)
class Assessment:
    evaluations: tuple[SignalEvaluation, ...]
    # Written into the detail of the step that ran them, because a skipped signal has no row (ADR-0044, ADR-0045).
    skipped: tuple[TransferSignalType, ...]
    # The first that fired in ORDER, or None: fired already means over the threshold with the policy on.
    winner: SignalEvaluation | None

    def of(self, signal: TransferSignalType) -> SignalEvaluation | None:
        return next((e for e in self.evaluations if e.signal_type == signal), None)


# Every signal is evaluated even when one already fired, because cutting at the first is what leaves OE4 without data (ADR-0014).
def assess(
    *,
    message_batch_id: UUID,
    verdict: Verdict | None,
    classifier_version: str,
    policies: Mapping[TransferSignalType, TransferPolicy],
    defaults: Mapping[TransferSignalType, int],
    previous_streak: int,
) -> Assessment:
    if verdict is None:
        # No verdict is no call to the provider: nothing was judged, so nothing is written as if it had been.
        return Assessment(evaluations=(), skipped=ORDER, winner=None)

    def evaluate(signal: TransferSignalType, score: Decimal, threshold: Decimal) -> SignalEvaluation:
        policy = policies.get(signal)
        return SignalEvaluation(
            id=ids.new(),
            message_batch_id=message_batch_id,
            signal_type=signal,
            score=score,
            threshold_used=threshold,
            classifier_version=classifier_version,
            fired=score >= threshold and policy is not None and policy.enabled,
        )

    evaluations = [
        evaluate(TransferSignalType.OUT_OF_DOMAIN, _YES if verdict.label == "out_of_domain" else _NO, _YES),
    ]
    # frustration waits for its model until Fase 16, and is never written as a no nobody measured (ADR-0044, section 2.3).
    skipped = [TransferSignalType.FRUSTRATION]

    if verdict.explicit_request is None:
        skipped.append(TransferSignalType.EXPLICIT_REQUEST)
    else:
        evaluations.append(evaluate(TransferSignalType.EXPLICIT_REQUEST, _YES if verdict.explicit_request else _NO, _YES))

    if verdict.resolved is None:
        # The first turn, or an answer the AIAgent did not write: there is nothing of its own to judge (ADR-0045, section 2.2).
        skipped.append(TransferSignalType.NO_RESOLUTION)
    else:
        streak = 0 if verdict.resolved else previous_streak + 1
        policy = policies.get(TransferSignalType.NO_RESOLUTION)
        threshold = policy.threshold if policy is not None else Decimal(defaults[TransferSignalType.NO_RESOLUTION])
        evaluations.append(evaluate(TransferSignalType.NO_RESOLUTION, Decimal(streak), threshold))

    fired = {e.signal_type: e for e in evaluations if e.fired}
    winner = next((fired[s] for s in ORDER if s in fired), None)
    return Assessment(
        evaluations=tuple(evaluations),
        skipped=tuple(s for s in ORDER if s in skipped),
        winner=winner,
    )
