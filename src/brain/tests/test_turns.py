# The contract of 07-secuencia section 1, exercised without PostgreSQL: ADR-0028 asks for doubled dependencies from day one.

from contextlib import asynccontextmanager
from datetime import UTC, datetime
from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from infra.config import Settings
from main import create_app
from pipeline.repository import TurnNotFound, read_turn

BATCH_ID = uuid4()
CONVERSATION_ID = uuid4()
AGENT_ID = uuid4()


class FakeCursor:
    def __init__(self, rows: list[dict]) -> None:
        self._rows = rows

    async def fetchone(self) -> dict | None:
        return self._rows[0] if self._rows else None

    async def fetchall(self) -> list[dict]:
        return self._rows


# Keyed by the table each statement hits, so reordering the reads does not silently rewrite what the test asserts.
class FakeConnection:
    def __init__(self, tables: dict[str, list[dict]]) -> None:
        self.tables = tables
        self.seen: list[str] = []

    async def execute(self, sql: str, params: dict | None = None) -> FakeCursor:
        for table, rows in self.tables.items():
            if table in sql:
                self.seen.append(table)
                return FakeCursor(rows)
        return FakeCursor([])


class FakeDatabase:
    def __init__(self, connection: FakeConnection) -> None:
        self._connection = connection

    def connection(self):
        database = self

        class _Scope:
            async def __aenter__(self):
                return database._connection

            async def __aexit__(self, *_: object) -> bool:
                return False

        return _Scope()


def message(text: str, direction: str = "inbound", sender: str = "contact") -> dict:
    return {
        "direction": direction,
        "sender_type": sender,
        "content_text": text,
        "sent_at": datetime.now(UTC),
    }


def tables(**overrides: list[dict]) -> dict[str, list[dict]]:
    base = {
        "message_batches": [{"id": BATCH_ID, "conversation_id": CONVERSATION_ID, "turn_number": 1}],
        "ai_agents": [
            {
                "id": AGENT_ID,
                "name": "Agente demo",
                "llm_provider": "cohere",
                "llm_model": "command-a-03-2025",
                "temperature": None,
                "max_tokens": 512,
            }
        ],
        "messages": [message("hola")],
        # The six of F01: without them the agent reaches the model with no instructions at all (Regla 1).
        "prompt_components": [
            {"type": "objective", "content": "Ayudar a inquilinos.", "position": 1, "version_id": uuid4(), "read_at": datetime.now(UTC)},
            {"type": "rules", "content": "No tomar pagos por este canal.", "position": 2, "version_id": uuid4(), "read_at": datetime.now(UTC)},
        ],
        # Declared so the fake connection records the INSERTs: the two writes are the only ones the brain makes in a turn.
        "generations": [],
        "generation_components": [],
    }
    base.update(overrides)
    return base


# Replaces the startup that opens a real psycopg pool, which would overwrite the double these cases run on.
@asynccontextmanager
async def _no_lifespan(_app):
    yield


def client(connection: FakeConnection, settings: Settings) -> TestClient:
    app = create_app(settings)
    app.router.lifespan_context = _no_lifespan
    app.state.database = FakeDatabase(connection)
    # Server exceptions are left to raise: an unexpected 500 has to break the test and not become an assertion about a body.
    return TestClient(app)


def body(**overrides: object) -> dict:
    payload = {
        "message_batch_id": str(BATCH_ID),
        "conversation_id": str(CONVERSATION_ID),
        "ai_agent_id": str(AGENT_ID),
        "mode": "autonomous",
    }
    payload.update(overrides)
    return payload


# The shared secret is the whole authentication between the two deployables (ADR-0017 (k)), so an unsigned call must not read a turn.
def test_turns_rejects_a_caller_that_is_not_the_core(settings: Settings) -> None:
    connection = FakeConnection(tables())

    with client(connection, settings) as http:
        response = http.post("/turns", json=body())

    assert response.status_code == 401
    assert connection.seen == [], "an unauthenticated call reached the database"


def test_turns_answers_the_contract_of_section_1(settings: Settings) -> None:
    connection = FakeConnection(tables())

    with client(connection, settings) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.status_code == 200
    payload = response.json()
    assert set(payload) == {"outcome", "generation_id", "completion_text", "transfer"}
    # failed and never blocked: this deployment has no key for the provider the row names, which is not a rejection.
    assert payload["outcome"] == "failed"
    assert payload["transfer"] is None


# The whole path with both ends doubled, which is what the CI smoke test runs: a turn in, a generated answer out.
def test_turns_generates_when_a_provider_answers() -> None:
    connection = FakeConnection(tables())
    doubled = Settings(
        _env_file=None,
        database_url="postgresql://sonia:sonia@127.0.0.1:5432/sonia",
        brain_internal_token="test-token",
        environment="test",
        llm_use_double=True,
    )

    with client(connection, doubled) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    payload = response.json()
    assert payload["outcome"] == "generated"
    # The double echoes the last user message, so the answer proves the assembled envelope is what reached the model.
    assert payload["completion_text"].endswith("hola"), "the answer has to carry the message that arrived"
    assert "[MENSAJE DEL CLIENTE]" in payload["completion_text"], "the turn reached the model without the envelope of Regla 1"
    # Present because the evidence exists: it is the id the core writes back into generations.message_id.
    assert payload["generation_id"] is not None
    assert "generations" in connection.seen, "the turn generated an answer and left no evidence of it"
    # One row per component assembled: without them OE1 reads this generation as missing all of them.
    assert connection.seen.count("generation_components") == 2, "the generation does not name what it assembled"


# The fixture has two of the six: the turn answers anyway, and says so in the log.
def test_a_turn_with_fewer_than_six_components_says_so(settings: Settings, caplog: pytest.LogCaptureFixture) -> None:
    connection = FakeConnection(tables())

    with caplog.at_level("INFO", logger="sonia"), client(connection, settings) as http:
        http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert [r for r in caplog.records if "fewer components" in r.getMessage()]


def test_turns_reads_the_batch_the_agent_and_the_messages(settings: Settings) -> None:
    connection = FakeConnection(tables())

    with client(connection, settings) as http:
        http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert connection.seen == [
        "message_batches",
        "ai_agents",
        "messages",
        "messages",
        "prompt_components",
    ], "the brain did not read the turn from PostgreSQL, which is what section 1 replaces the payload with"


# Four ids that name nothing is the caller's mistake; a 500 would tell the core to retry something that cannot improve.
@pytest.mark.parametrize(
    ("missing", "reason"),
    [
        ("message_batches", "a batch that does not exist"),
        ("ai_agents", "an agent that was deleted"),
    ],
)
def test_turns_reports_a_turn_it_cannot_find(settings: Settings, missing: str, reason: str) -> None:
    connection = FakeConnection(tables(**{missing: []}))

    with client(connection, settings) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.status_code == 404, reason
    assert response.json()["detail"]["code"] == "turn_not_found"


# off never reaches the brain: the core does not call for a conversation whose agent is off, and the schema says so.
@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("mode", "off"),
        ("mode", "autonomo"),
        ("conversation_id", "not-a-uuid"),
    ],
)
def test_turns_refuses_a_request_outside_the_contract(settings: Settings, field: str, value: str) -> None:
    connection = FakeConnection(tables())

    with client(connection, settings) as http:
        response = http.post(
            "/turns", json=body(**{field: value}), headers={"X-Internal-Token": "test-token"}
        )

    assert response.status_code == 422
    assert connection.seen == [], "an invalid request reached the database"


def test_turns_refuses_a_field_the_contract_does_not_declare(settings: Settings) -> None:
    connection = FakeConnection(tables())

    with client(connection, settings) as http:
        response = http.post(
            "/turns",
            json=body(completion_text="inyectado"),
            headers={"X-Internal-Token": "test-token"},
        )

    assert response.status_code == 422, "extra=forbid is what keeps the contract from drifting silently"


# The prompt has to read in the order the conversation happened, and the query returns the newest first to make the limit useful.
async def test_read_turn_returns_history_oldest_first() -> None:
    connection = FakeConnection(
        tables(messages=[message("tercero"), message("segundo"), message("primero")])
    )

    turn = await read_turn(connection, BATCH_ID, CONVERSATION_ID, AGENT_ID)

    assert [m.content_text for m in turn.history] == ["primero", "segundo", "tercero"]
    assert turn.turn_number == 1
    assert turn.agent.llm_model == "command-a-03-2025"


async def test_read_turn_names_the_batch_it_could_not_find() -> None:
    connection = FakeConnection(tables(message_batches=[]))

    with pytest.raises(TurnNotFound) as raised:
        await read_turn(connection, BATCH_ID, CONVERSATION_ID, AGENT_ID)

    assert raised.value.message_batch_id == BATCH_ID


# A model with no tariff fails the turn instead of writing a cost nobody can defend into the table OE2 reads.
def test_turns_fails_when_the_model_has_no_tariff() -> None:
    agent = tables()["ai_agents"][0] | {"llm_model": "un-modelo-que-nadie-tarifo"}
    connection = FakeConnection(tables(ai_agents=[agent]))
    doubled = Settings(
        _env_file=None,
        database_url="postgresql://sonia:sonia@127.0.0.1:5432/sonia",
        brain_internal_token="test-token",
        environment="test",
        llm_use_double=True,
    )

    with client(connection, doubled) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.status_code == 200, "a configuration error is not a reason to tell the core to retry"
    assert response.json()["outcome"] == "failed"
    assert "generations" not in connection.seen, "a turn that never ran left evidence saying it did"
