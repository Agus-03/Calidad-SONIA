# The doubles of POST /turns, shared by its four test files: one turn, one fake connection and the doubled stack (ADR-0028).

from contextlib import asynccontextmanager
from datetime import UTC, datetime
from decimal import Decimal
from uuid import uuid4

from fastapi.testclient import TestClient

from infra.config import Settings
from main import create_app


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
        # What each statement carried, keyed by the table it hit: asserting that a value never reached a column needs the value.
        self.sent: list[tuple[str, dict]] = []

    async def execute(self, sql: str, params: dict | None = None) -> FakeCursor:
        # Longest name first, so a statement that joins two tables is keyed by the one it is about and not by dict order.
        for table, rows in sorted(self.tables.items(), key=lambda item: -len(item[0])):
            if table in sql:
                self.seen.append(table)
                self.sent.append((table, params or {}))
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
        # Its own id, which is what an extracted datum names as the message it was said in (ADR-0047).
        "id": uuid4(),
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
        # Empty by default: an account with no catalogue is the common case, and F07 adds nothing to its prompt (ADR-0061).
        "quick_replies": [],
        # Declared so the fake connection records the INSERTs: these are the only writes the brain makes in a turn.
        "detected_intents": [],
        "pipeline_steps": [],
        "generations": [],
        "generation_components": [],
        # The fifth, and only on a cut the triage decided (O1c). transfer_policies is left out on purpose: absent means the
        # administrator never configured this signal, which is the case a turn has to survive.
        "signal_evaluations": [],
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
def policy(signal: str, threshold: int = 1, enabled: bool = True) -> dict:
    return {"signal_type": signal, "enabled": enabled, "threshold": Decimal(threshold)}


def doubled() -> Settings:
    return Settings(
        _env_file=None,
        database_url="postgresql://sonia:sonia@127.0.0.1:5432/sonia",
        brain_internal_token="test-token",
        environment="test",
        llm_use_double=True,
    )
