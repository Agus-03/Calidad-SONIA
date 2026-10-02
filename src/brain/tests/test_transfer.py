# The signals of F12 as rows of signal_evaluations and the attribution between them (ADR-0045, ADR-0021, ADR-0044), with no
# connection: the rules are the ones that decide what OE3 and OE4 get to read.

from decimal import Decimal
from uuid import uuid4

import pytest

from classifiers import transfer
from infra.enums import TransferSignalType
from pipeline.repository.signals import TransferPolicy
from pipeline.triage import Verdict

DEFAULTS = {
    TransferSignalType.OUT_OF_DOMAIN: 1,
    TransferSignalType.EXPLICIT_REQUEST: 1,
    TransferSignalType.NO_RESOLUTION: 3,
}
ON = TransferPolicy(enabled=True, threshold=Decimal(1))


def verdict(
    label: str = "in_domain", *, explicit_request: bool | None = False, resolved: bool | None = True
) -> Verdict:
    return Verdict(
        label=label,
        confidence=Decimal("0.9"),
        greeting=False,
        explicit_request=explicit_request,
        resolved=resolved,
    )


def assess(v: Verdict | None, *, policies=None, streak: int = 0) -> transfer.Assessment:
    return transfer.assess(
        message_batch_id=uuid4(),
        verdict=v,
        classifier_version="2",
        policies=policies or {},
        defaults=DEFAULTS,
        previous_streak=streak,
    )


def by_type(assessment: transfer.Assessment) -> dict[TransferSignalType, object]:
    return {e.signal_type: e for e in assessment.evaluations}


# ADR-0014: every signal is written whether it fired or not, or the only sample left is the one the threshold filtered.
def test_every_judged_signal_is_written_even_when_nothing_fired() -> None:
    result = assess(verdict())

    assert set(by_type(result)) == {
        TransferSignalType.OUT_OF_DOMAIN,
        TransferSignalType.EXPLICIT_REQUEST,
        TransferSignalType.NO_RESOLUTION,
    }
    assert result.winner is None
    assert all(e.classifier_version == "2" for e in result.evaluations)


# ADR-0044, section 2.3: frustration is skipped in every turn until its model exists, and never written as a no.
def test_frustration_is_skipped_and_never_written() -> None:
    result = assess(verdict())

    assert TransferSignalType.FRUSTRATION in result.skipped
    assert TransferSignalType.FRUSTRATION not in by_type(result)


# ADR-0045, section 2.4: a yes/no signal is 1 or 0 against 1, so threshold_used means one thing in every row.
@pytest.mark.parametrize(("label", "score"), [("out_of_domain", Decimal(1)), ("in_domain", Decimal(0))])
def test_a_yes_no_signal_is_one_or_zero_against_one(label: str, score: Decimal) -> None:
    row = by_type(assess(verdict(label)))[TransferSignalType.OUT_OF_DOMAIN]

    assert row.score == score
    assert row.threshold_used == Decimal(1)


# fired is over the threshold and the policy on: with no policy nobody decided to transfer for it (ADR-0045, section 2.4).
def test_a_signal_over_the_line_with_no_policy_does_not_fire() -> None:
    row = by_type(assess(verdict(explicit_request=True)))[TransferSignalType.EXPLICIT_REQUEST]

    assert row.score == Decimal(1)
    assert row.fired is False


def test_a_signal_over_the_line_with_its_policy_off_does_not_fire() -> None:
    off = {TransferSignalType.EXPLICIT_REQUEST: TransferPolicy(enabled=False, threshold=Decimal(1))}

    assert by_type(assess(verdict(explicit_request=True), policies=off))[TransferSignalType.EXPLICIT_REQUEST].fired is False


# ADR-0021: explicit_request goes first, because it is the one signal that is not an inference of ours.
def test_the_first_signal_in_the_fixed_order_wins() -> None:
    policies = {TransferSignalType.EXPLICIT_REQUEST: ON, TransferSignalType.OUT_OF_DOMAIN: ON}

    result = assess(verdict("out_of_domain", explicit_request=True), policies=policies)

    assert result.winner is not None
    assert result.winner.signal_type == TransferSignalType.EXPLICIT_REQUEST
    # Attribution is a reading and not a loss: the other one that fired is still written as fired.
    assert by_type(result)[TransferSignalType.OUT_OF_DOMAIN].fired is True


# ADR-0045, section 2.2: the score of no_resolution is the run of turns the AIAgent did not resolve.
def test_an_unresolved_turn_continues_the_run() -> None:
    row = by_type(assess(verdict(resolved=False), streak=1))[TransferSignalType.NO_RESOLUTION]

    assert row.score == Decimal(2)


def test_a_resolved_turn_ends_the_run() -> None:
    row = by_type(assess(verdict(resolved=True), streak=5))[TransferSignalType.NO_RESOLUTION]

    assert row.score == Decimal(0)


def test_the_run_fires_when_it_reaches_the_threshold_of_the_policy() -> None:
    policies = {TransferSignalType.NO_RESOLUTION: TransferPolicy(enabled=True, threshold=Decimal(2))}

    result = assess(verdict(resolved=False), policies=policies, streak=1)

    assert result.winner is not None
    assert result.winner.signal_type == TransferSignalType.NO_RESOLUTION
    assert result.winner.threshold_used == Decimal(2)


# With no policy the threshold is the default of triage.yaml, and never the 0 that read as "compared against nothing".
def test_with_no_policy_the_threshold_is_the_default_of_the_file() -> None:
    row = by_type(assess(verdict(resolved=False)))[TransferSignalType.NO_RESOLUTION]

    assert row.threshold_used == Decimal(3)


def test_with_no_answer_to_judge_no_resolution_is_skipped() -> None:
    result = assess(verdict(resolved=None))

    assert TransferSignalType.NO_RESOLUTION in result.skipped
    assert TransferSignalType.NO_RESOLUTION not in by_type(result)


def test_a_request_the_model_did_not_judge_is_skipped() -> None:
    result = assess(verdict(explicit_request=None))

    assert TransferSignalType.EXPLICIT_REQUEST in result.skipped
    assert TransferSignalType.EXPLICIT_REQUEST not in by_type(result)


# No verdict is no call to the provider: an agent with no objective has nothing judged, and nothing is written as if it had.
def test_no_verdict_skips_every_signal() -> None:
    result = assess(None)

    assert result.evaluations == ()
    assert result.skipped == transfer.ORDER
    assert result.winner is None
