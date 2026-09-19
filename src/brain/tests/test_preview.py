# The preview of F03 without PostgreSQL: the same assembler as the turn, over the drafts the panel sends (ADR-0040).

from contextlib import asynccontextmanager
from datetime import UTC, datetime
from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from infra.config import Settings
from infra.enums import PromptComponentType
from main import create_app
from pipeline.generation import UnversionedComponent, generate
from prompts.assembler import assemble, rules
from prompts.repository import Component
from tests.test_generation import FakeClient, turn

AGENT_ID = uuid4()
TOKEN = {"X-Internal-Token": "test-token"}


class FakeCursor:
    def __init__(self, row: dict | None) -> None:
        self._row = row

    async def fetchone(self) -> dict | None:
        return self._row


# Records every statement, so a case can prove the preview only reads.
class FakeConnection:
    def __init__(self, name: str | None) -> None:
        self.name = name
        self.sql: list[str] = []

    async def execute(self, sql: str, params: dict | None = None) -> FakeCursor:
        self.sql.append(sql)
        return FakeCursor({"name": self.name} if self.name is not None else None)


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


@asynccontextmanager
async def _no_lifespan(_app):
    yield


def client(connection: FakeConnection, settings: Settings) -> TestClient:
    app = create_app(settings)
    app.router.lifespan_context = _no_lifespan
    app.state.database = FakeDatabase(connection)
    return TestClient(app)


def draft(kind: str, content: str, position: int) -> dict:
    return {"type": kind, "content": content, "position": position}


def ask(settings: Settings, components: list[dict], name: str | None = "Agente demo", **extra: object):
    connection = FakeConnection(name)
    with client(connection, settings) as http:
        response = http.post(
            "/prompts/preview",
            json={"ai_agent_id": str(AGENT_ID), "components": components, **extra},
            headers=TOKEN,
        )
    return response, connection


# The order of the prompt is position, whatever order the body lists them in: the same order the turn's query gives.
def test_the_preview_assembles_the_drafts_by_their_position(settings: Settings) -> None:
    response, _ = ask(settings, [draft("rules", "Sin pagos.", 6), draft("objective", "Ayudar.", 1)])

    assert response.status_code == 200
    payload = response.json()
    assert payload["components"] == ["objective", "rules"]
    assert payload["system"].index("Ayudar.") < payload["system"].index("Sin pagos.")
    assert payload["rules_version"] == rules().version


# The whole point of one assembler (ADR-0017): the administrator reads the exact system the model reads for the same blocks.
def test_the_preview_is_the_system_a_turn_would_send(settings: Settings) -> None:
    blocks = [draft("objective", "Ayudar.", 1), draft("tone", "Formal pero cálido.", 3)]

    response, _ = ask(settings, blocks)

    turn_prompt = assemble(
        agent_name="Agente demo",
        components=[Component(PromptComponentType(b["type"]), b["content"], b["position"], uuid4()) for b in blocks],
        message="hola",
    )
    assert response.json()["system"] == turn_prompt.system


# The envelope is not previewed (ADR-0040): the answer carries the system and nothing of a customer's message.
def test_the_preview_answers_the_system_alone(settings: Settings) -> None:
    response, _ = ask(settings, [draft("objective", "Ayudar.", 1)])

    assert set(response.json()) == {"system", "rules_version", "components"}
    assert rules().user_headings["message"] not in response.json()["system"]


# A blank draft is what the administrator sees after emptying a block: its section goes, the same as in a turn.
def test_a_blank_draft_leaves_its_section_out(settings: Settings) -> None:
    response, _ = ask(settings, [draft("objective", "Ayudar.", 1), draft("rules", "   ", 6)])

    assert response.json()["components"] == ["objective"]
    assert rules().headings[PromptComponentType.RULES] not in response.json()["system"]


# The shortfall is news in a turn and noise in a preview, which runs on every keystroke (ADR-0040).
def test_a_preview_with_fewer_than_six_logs_nothing(settings: Settings, caplog: pytest.LogCaptureFixture) -> None:
    with caplog.at_level("INFO", logger="sonia"):
        ask(settings, [draft("objective", "Ayudar.", 1)])

    assert not [r for r in caplog.records if "fewer components" in r.getMessage()]


# A preview is not evidence: it reads the agent's name and writes nothing (regla 3).
def test_the_preview_only_reads(settings: Settings) -> None:
    _, connection = ask(settings, [draft("objective", "Ayudar.", 1)])

    assert len(connection.sql) == 1
    assert connection.sql[0].lstrip().startswith("SELECT")
    assert "a.deleted_at IS NULL" in connection.sql[0]


# The core checked the agent before asking, so a 404 here is an agent removed in between, and it is named.
def test_a_removed_agent_is_a_404(settings: Settings) -> None:
    response, _ = ask(settings, [draft("objective", "Ayudar.", 1)], name=None)

    assert response.status_code == 404
    assert response.json()["detail"]["code"] == "ai_agent_not_found"


# The rules of a save (ADR-0039): a preview never shows a prompt the panel could not save.
@pytest.mark.parametrize(
    ("components", "extra", "reason"),
    [
        ([draft("tone", "a", 1), draft("tone", "b", 2)], {}, "two of one type"),
        ([draft("tone", "a", 1), draft("rules", "b", 1)], {}, "two at one position"),
        ([draft("greeting", "a", 1)], {}, "a type F01 does not declare"),
        ([draft("tone", "a", 0)], {}, "a position below 1"),
        ([draft("tone", "a", 40000)], {}, "a position the column cannot hold"),
        ([{**draft("tone", "a", 1), "active": True}], {}, "a field a draft does not have"),
        ([draft("tone", "a", 1)], {"message": "hola"}, "a field the request does not have"),
    ],
)
def test_a_draft_that_could_not_be_saved_is_refused(settings: Settings, components: list, extra: dict, reason: str) -> None:
    response, _ = ask(settings, components, **extra)

    assert response.status_code == 422, reason


# The other side of the optional version: a draft can be previewed, and never assembled into a generation (ADR-0040).
async def test_a_draft_never_becomes_evidence() -> None:
    prompt = assemble(
        agent_name="Agente demo",
        components=(Component(PromptComponentType.OBJECTIVE, "Ayudar.", 1),),
        message="hola",
    )
    client = FakeClient()

    with pytest.raises(UnversionedComponent):
        await generate(client=client, turn=turn(), prompt=prompt, assembled_at=datetime.now(UTC))

    assert client.calls == [], "the turn paid for a call it could not leave evidence of"
