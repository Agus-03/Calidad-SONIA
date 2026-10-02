# The signals of F12: what the administrator configured, the run they continue and what each turn evaluated (ADR-0045).

from dataclasses import dataclass
from decimal import Decimal
from uuid import UUID

from psycopg import AsyncConnection

from infra.enums import TransferSignalType
from pipeline.repository.turn import Turn


# What the administrator decided about one of the four signals of F12. Read and never written here: transfer_policies is
# configuration of the core (docs/struct.md), and regla 3 lets the brain write evidence and nothing else.
@dataclass(frozen=True)
class TransferPolicy:
    enabled: bool
    threshold: Decimal


# Oldest first, so the newest row of a signal wins below: nothing in 0001 makes (ai_agent_id, signal_type) unique.
_POLICIES = """
SELECT signal_type, enabled, threshold
  FROM transfer_policies
 WHERE ai_agent_id = %(ai_agent_id)s
   AND deleted_at IS NULL
 ORDER BY created_at, id
"""


# A signal missing from the answer has no policy, which is not the same as one that is disabled: nobody configured it. Either way
# it proposes nothing — what is lost is the transfer_events row, which is the escalation F36 counts (ADR-0045, section 2.4).
async def read_transfer_policies(
    conn: AsyncConnection, ai_agent_id: UUID
) -> dict[TransferSignalType, TransferPolicy]:
    cursor = await conn.execute(_POLICIES, {"ai_agent_id": ai_agent_id})
    return {
        TransferSignalType(row["signal_type"]): TransferPolicy(enabled=row["enabled"], threshold=row["threshold"])
        for row in await cursor.fetchall()
    }


# The run of the batch just before this one, which only continues under the same prompt and when that batch did not end in a
# transfer: a conversation an Operator turned back on starts again from zero (ADR-0045, section 2.2).
_NO_RESOLUTION_STREAK = """
SELECT se.score
  FROM signal_evaluations se
  JOIN message_batches b ON b.id = se.message_batch_id
 WHERE b.conversation_id = %(conversation_id)s
   AND b.turn_number = %(turn_number)s - 1
   AND se.signal_type = %(signal_type)s
   AND se.classifier_version = %(classifier_version)s
   AND NOT EXISTS (
       SELECT 1 FROM transfer_events te
        WHERE te.conversation_id = b.conversation_id AND te.turn_number = b.turn_number)
"""


async def read_no_resolution_streak(conn: AsyncConnection, turn: Turn, classifier_version: str) -> int:
    cursor = await conn.execute(
        _NO_RESOLUTION_STREAK,
        {
            "conversation_id": turn.conversation_id,
            "turn_number": turn.turn_number,
            "signal_type": TransferSignalType.NO_RESOLUTION,
            "classifier_version": classifier_version,
        },
    )
    row = await cursor.fetchone()
    return int(row["score"]) if row is not None else 0


# One evaluated signal of F12, whether it fired or not. That is the whole point of the table: a classifier whose precision is an
# objective has to persist its output even when nothing happened, or the only sample left is the one the threshold already filtered.
@dataclass(frozen=True)
class SignalEvaluation:
    id: UUID
    message_batch_id: UUID
    signal_type: TransferSignalType
    # 1 or 0 for a yes/no signal, and the run of turns for no_resolution (ADR-0045, section 2.4).
    score: Decimal
    # What score was compared against, frozen so moving the threshold later does not rewrite what was measured: 1 for a yes/no
    # signal, the policy's for no_resolution, or the default of triage.yaml when there is no policy (ADR-0045, section 2.4).
    threshold_used: Decimal
    classifier_version: str
    # Not derivable from score and threshold: a signal can be over the line and not fire, because the policy is disabled or
    # because there is no policy at all.
    fired: bool


_SIGNALS_OF_BATCH = """
DELETE FROM signal_evaluations
 WHERE message_batch_id = %(message_batch_id)s
   AND signal_type = %(signal_type)s
"""

_SIGNAL = """
INSERT INTO signal_evaluations (
    id, message_batch_id, signal_type, score, threshold_used, classifier_version, fired
) VALUES (
    %(id)s, %(message_batch_id)s, %(signal_type)s, %(score)s, %(threshold_used)s, %(classifier_version)s, %(fired)s
)
"""


# Deleted first like the steps and the intents: the core asks again on a 5xx (ADR-0020) and one turn leaves one evaluation per
# signal. By signal_type, because the other three of F12 belong to their own classifiers. A row transfer_events points at cannot
# be deleted, and that FK is doing its job: this turn fails loudly instead of duplicating evidence.
async def write_signal_evaluation(conn: AsyncConnection, evaluation: SignalEvaluation) -> None:
    await conn.execute(
        _SIGNALS_OF_BATCH,
        {"message_batch_id": evaluation.message_batch_id, "signal_type": evaluation.signal_type},
    )
    await conn.execute(
        _SIGNAL,
        {
            "id": evaluation.id,
            "message_batch_id": evaluation.message_batch_id,
            "signal_type": evaluation.signal_type,
            "score": evaluation.score,
            "threshold_used": evaluation.threshold_used,
            "classifier_version": evaluation.classifier_version,
            "fired": evaluation.fired,
        },
    )
