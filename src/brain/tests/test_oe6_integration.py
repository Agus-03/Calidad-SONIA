# OE6 calculable (ADR-0024, ADR-0075, ADR-0077): the query of docs/design/05-consultas-de-metricas.sql, read from the document
# itself and run against the real DDL on a scenario whose answer is known. If the document's query stops parsing, or stops
# counting what ADR-0075 and ADR-0077 say, this fails. One transaction, rolled back.

import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import UUID

import pytest
from psycopg.types.json import Jsonb

from infra import ids
from infra.db import Database

_DSN = os.getenv("SONIA_TEST_DATABASE_URL", "")

pytestmark = pytest.mark.skipif(
    not _DSN,
    reason="SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration",
)

# A closing time no real block has: the window holds this case's blocks and nobody else's, whatever runs beside it.
_DAY = datetime(2000, 1, 1, 12, tzinfo=UTC)

_QUERIES = Path(__file__).resolve().parents[3] / "docs" / "design" / "05-consultas-de-metricas.sql"


# The OE6 query as the document writes it, from its WITH to the end of its SELECT, with the two parameters in psycopg's form.
def _oe6() -> str:
    text = _QUERIES.read_text(encoding="utf-8")
    section = text[text.index("-- OE6 — 90 %") :]
    query = section[section.index("WITH escaladas_del_dominio") : section.index("FROM   consultas_del_dominio;") + len("FROM   consultas_del_dominio")]
    return query.replace(":desde", "%(desde)s").replace(":hasta", "%(hasta)s")


async def _agent(conn, account: UUID, *, with_base: bool, operator: UUID) -> UUID:
    agent = ids.new()
    await conn.execute(
        """INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
           VALUES (%s, %s, %s, 'autonomous', 'anthropic', 'claude-sonnet-5-5')""",
        (agent, account, f"OE6 {agent}"),
    )
    if with_base:
        base, document = ids.new(), ids.new()
        await conn.execute(
            "INSERT INTO knowledge_bases (id, ai_agent_id, collection_name) VALUES (%s, %s, %s)", (base, agent, f"kb_{agent}")
        )
        await conn.execute(
            """INSERT INTO documents (id, knowledge_base_id, filename, mime_type, size_bytes, checksum, object_key, status,
                                      uploaded_by_operator_id, personal_data_terms_version, created_at)
               VALUES (%s, %s, 'precios.pdf', 'application/pdf', 10, %s, %s, 'indexed', %s, 1, %s)""",
            (document, base, str(document), f"knowledge-bases/{base}/{document}", operator, _DAY - timedelta(days=1)),
        )
    return agent


# One closed block of its own conversation, with the triage's verdict and, when given, the retrieval's and the grounding's.
async def _block(conn, seeded: dict, agent: UUID, *after_triage: str) -> None:
    conversation, batch = ids.new(), ids.new()
    await conn.execute(
        """INSERT INTO conversations (id, contact_id, inbox_id, ai_agent_id, status, mode, last_inbound_at, window_expires_at)
           VALUES (%s, %s, %s, %s, 'resolved', 'autonomous', now(), now() + interval '24 hours')""",
        (conversation, seeded["contact"], seeded["inbox"], agent),
    )
    await conn.execute(
        """INSERT INTO message_batches (id, conversation_id, turn_number, timer_ms_used, expires_at, closed_at)
           VALUES (%s, %s, 1, 10000, %s, %s)""",
        (batch, conversation, _DAY, _DAY),
    )
    steps = [("triage", "in_domain", 3, {"mode": "autonomous"})]
    names = ("retrieval", "grounding", "generation")
    steps += [(step, outcome, position, None) for step, outcome, position in zip(names, after_triage, (4, 5, 6))]
    for step, outcome, position, detail in steps:
        await conn.execute(
            """INSERT INTO pipeline_steps (id, message_batch_id, step, direction, position, outcome, version, detail_json)
               VALUES (%s, %s, %s, %s, %s, %s, '1', %s)""",
            (
                ids.new(),
                batch,
                step,
                "outbound" if step == "generation" else "inbound",
                position,
                outcome,
                Jsonb(detail) if detail is not None else None,
            ),
        )


async def test_oe6_counts_the_not_grounded_and_the_failed_turns_as_not_answered_and_leaves_agents_without_a_base_out() -> None:
    database = Database(_DSN)
    await database.open()
    try:
        async with database.connection() as conn, conn.transaction(force_rollback=True):
            cursor = await conn.execute(
                "SELECT a.id AS account, c.id AS contact, i.id AS inbox FROM accounts a, contacts c, inboxes i LIMIT 1"
            )
            seeded = await cursor.fetchone()
            if seeded is None:
                pytest.skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
            operator = ids.new()
            await conn.execute(
                """INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
                   VALUES (%s, %s, 'Prueba', 'OE6', %s, 'x', 'admin', true)""",
                (operator, seeded["account"], f"{operator}@sonia.test"),
            )
            with_base = await _agent(conn, seeded["account"], with_base=True, operator=operator)
            without_base = await _agent(conn, seeded["account"], with_base=False, operator=operator)
            await _block(conn, seeded, with_base, "hit", "grounded", "ok")
            # Grounded, and the provider never answered: nobody got an answer (ADR-0077).
            await _block(conn, seeded, with_base, "hit", "grounded", "failed")
            # Not grounded with the policy off: no transfer_events, and still not answered (ADR-0075).
            await _block(conn, seeded, with_base, "hit", "not_grounded")
            # Voyage or Qdrant did not answer: the turn failed, and the block was not answered either (ADR-0077).
            await _block(conn, seeded, with_base, "failed")
            # 100 % by construction, so out of the measurement (ADR-0075).
            await _block(conn, seeded, without_base)

            cursor = await conn.execute(_oe6(), {"desde": _DAY - timedelta(hours=1), "hasta": _DAY + timedelta(hours=1)})
            row = await cursor.fetchone()

            assert (row["consultas_del_dominio"], row["respondidas_sin_escalada"], float(row["oe6_pct"])) == (4, 1, 25.0)
    finally:
        await database.close()
