# ADR-0068, reglas 6 and 7, against the real DDL: retrieval_logs hang from the retrieval row, a second run of the same batch
# replaces the trace with its logs instead of tripping the foreign key, and the grounding's out_of_domain row lives beside the
# triage's for the same batch. One transaction, rolled back.

import os
from decimal import Decimal
from uuid import UUID

import pytest

from infra import ids
from infra.db import Database
from infra.enums import MessageDirection, PipelineStep, TransferSignalType
from pipeline.repository.evidence import RetrievedChunk, TracedStep, write_steps
from pipeline.repository.signals import SignalEvaluation, write_signal_evaluation

_DSN = os.getenv("SONIA_TEST_DATABASE_URL", "")

pytestmark = pytest.mark.skipif(
    not _DSN,
    reason="SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration",
)


async def _batch_and_chunk(conn) -> tuple[UUID, UUID]:
    cursor = await conn.execute(
        """SELECT a.id AS account, c.id AS contact, i.id AS inbox
           FROM accounts a, contacts c, inboxes i LIMIT 1"""
    )
    seeded = await cursor.fetchone()
    if seeded is None:
        pytest.skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
    operator, agent, base, document, chunk = ids.new(), ids.new(), ids.new(), ids.new(), ids.new()
    conversation, batch = ids.new(), ids.new()
    await conn.execute(
        """INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
           VALUES (%s, %s, 'Prueba', 'Grounding', %s, 'x', 'admin', true)""",
        (operator, seeded["account"], f"{operator}@sonia.test"),
    )
    await conn.execute(
        """INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
           VALUES (%s, %s, %s, 'autonomous', 'anthropic', 'claude-sonnet-5-5')""",
        (agent, seeded["account"], f"Grounding {agent}"),
    )
    await conn.execute(
        "INSERT INTO knowledge_bases (id, ai_agent_id, collection_name) VALUES (%s, %s, %s)", (base, agent, f"kb_{agent}")
    )
    await conn.execute(
        """INSERT INTO documents (id, knowledge_base_id, filename, mime_type, size_bytes, checksum, object_key, status,
                                  uploaded_by_operator_id, personal_data_terms_version)
           VALUES (%s, %s, 'precios.pdf', 'application/pdf', 10, %s, %s, 'indexed', %s, 1)""",
        (document, base, str(document), f"knowledge-bases/{base}/{document}", operator),
    )
    await conn.execute(
        """INSERT INTO document_chunks (id, document_id, chunk_index, content, token_count, vector_id, chunking_version,
                                        embedding_model)
           VALUES (%s, %s, 0, 'Alquiler: $320.000 + expensas.', 8, %s, 1, 'double')""",
        (chunk, document, str(chunk)),
    )
    # Resolved, so the one-open-conversation-per-contact index of the seeded contact is not in the way.
    await conn.execute(
        """INSERT INTO conversations (id, contact_id, inbox_id, ai_agent_id, status, mode, last_inbound_at, window_expires_at)
           VALUES (%s, %s, %s, %s, 'resolved', 'autonomous', now(), now() + interval '24 hours')""",
        (conversation, seeded["contact"], seeded["inbox"], agent),
    )
    await conn.execute(
        """INSERT INTO message_batches (id, conversation_id, turn_number, timer_ms_used, expires_at)
           VALUES (%s, %s, 1, 10000, now())""",
        (batch, conversation),
    )
    return batch, chunk


def _steps(chunk: UUID) -> tuple[TracedStep, ...]:
    return (
        TracedStep(PipelineStep.TRIAGE, MessageDirection.INBOUND, 3, "in_domain", "1", 10, Decimal("0.9")),
        TracedStep(
            PipelineStep.RETRIEVAL,
            MessageDirection.INBOUND,
            4,
            "hit",
            "1",
            40,
            detail={"top_k": 3, "threshold": "0.5", "stale_documents": 0, "dropped": 0},
            retrieved=(RetrievedChunk(chunk, Decimal("0.300000"), 1),),
        ),
        TracedStep(PipelineStep.GROUNDING, MessageDirection.INBOUND, 5, "not_grounded", "1", 0, Decimal("0.300000")),
    )


def _out_of_domain(batch: UUID, version: str, score: Decimal) -> SignalEvaluation:
    return SignalEvaluation(ids.new(), batch, TransferSignalType.OUT_OF_DOMAIN, score, Decimal("0.5"), version, False)


async def test_the_logs_hang_from_the_retrieval_row_and_a_second_run_replaces_them() -> None:
    database = Database(_DSN)
    await database.open()
    try:
        async with database.connection() as conn, conn.transaction(force_rollback=True):
            batch, chunk = await _batch_and_chunk(conn)

            # Twice, as the core asks again after a 5xx (ADR-0020): the logs of the first run must not block the second.
            await write_steps(conn, batch, _steps(chunk))
            await write_steps(conn, batch, _steps(chunk))

            cursor = await conn.execute(
                """SELECT s.step, l.document_chunk_id, l.similarity_score, l.rank
                   FROM retrieval_logs l JOIN pipeline_steps s ON s.id = l.pipeline_step_id
                   WHERE s.message_batch_id = %s""",
                (batch,),
            )
            assert await cursor.fetchall() == [
                {"step": "retrieval", "document_chunk_id": chunk, "similarity_score": Decimal("0.300000"), "rank": 1}
            ]
            cursor = await conn.execute("SELECT count(*) AS n FROM pipeline_steps WHERE message_batch_id = %s", (batch,))
            assert (await cursor.fetchone())["n"] == 3

            # The triage's and the grounding's rows for the same signal, each written twice: one of each is left.
            for _ in range(2):
                await write_signal_evaluation(conn, _out_of_domain(batch, "1", Decimal(0)))
                await write_signal_evaluation(conn, _out_of_domain(batch, "retrieval1", Decimal("0.7")))
            cursor = await conn.execute(
                """SELECT classifier_version, score FROM signal_evaluations
                   WHERE message_batch_id = %s ORDER BY classifier_version""",
                (batch,),
            )
            assert await cursor.fetchall() == [
                {"classifier_version": "1", "score": Decimal(0)},
                {"classifier_version": "retrieval1", "score": Decimal("0.7")},
            ]
    finally:
        await database.close()
