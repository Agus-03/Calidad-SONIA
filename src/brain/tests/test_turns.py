# The contract of 07-secuencia section 1, exercised without PostgreSQL: ADR-0028 asks for doubled dependencies from day one.

import logging
from contextlib import asynccontextmanager
from datetime import UTC, datetime
from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from adapters.llm.base import Completion, LLMUnavailable
from infra.config import Settings
from main import create_app
from infra.enums import MessageDirection, PipelineStep
from pipeline import runner
from pipeline.repository import TurnNotFound, read_turn
from pipeline.step import Step, StepRequest, StepResult

BATCH_ID = uuid4()
CONVERSATION_ID = uuid4()
AGENT_ID = uuid4()

# A real CBU, because the outbound guardrail is only worth testing against what it is meant to catch.
CBU = "0170099220000067797151"


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
        for table, rows in self.tables.items():
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
        # Declared so the fake connection records the INSERTs: these are the only writes the brain makes in a turn.
        "detected_intents": [],
        "pipeline_steps": [],
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


# The fixture has two of the six: the turn answers anyway, and says so in the log. On the doubled stack, because the line it
# asserts is written after the assembly and the assembly now happens after a step that calls a provider.
def test_a_turn_with_fewer_than_six_components_says_so(caplog: pytest.LogCaptureFixture) -> None:
    connection = FakeConnection(tables())

    with caplog.at_level("INFO", logger="sonia"), client(connection, doubled()) as http:
        http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert [r for r in caplog.records if "fewer components" in r.getMessage()]


def test_turns_reads_the_batch_the_agent_and_the_messages(settings: Settings) -> None:
    connection = FakeConnection(tables())

    with client(connection, settings) as http:
        http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert connection.seen[:5] == [
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


# doubled is the whole of F11 against the doubled adapter: the classifier answers two intentions, they are written as evidence,
# and they reach the model that reads the message (ADR-0041, regla 12).
def doubled() -> Settings:
    return Settings(
        _env_file=None,
        database_url="postgresql://sonia:sonia@127.0.0.1:5432/sonia",
        brain_internal_token="test-token",
        environment="test",
        llm_use_double=True,
    )


def test_the_intents_of_the_batch_are_written_and_travel_to_the_prompt() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    payload = response.json()
    assert payload["outcome"] == "generated"
    # One DELETE plus one INSERT per intention: a second run of the same batch leaves one classification and not two.
    assert connection.seen.count("detected_intents") == 3, "the intents of F11 left no evidence"
    # The double echoes the envelope, so the answer is what proves the list reached the model and not only the database.
    assert "[LO QUE PIDIÓ EL CLIENTE, EN ORDEN]" in payload["completion_text"]
    assert "1. consulta_disponibilidad" in payload["completion_text"]
    assert "2. consulta_precio" in payload["completion_text"]


# The evidence belongs to the turn that used it: intents written after the generation would survive a turn that never answered.
def test_the_intents_are_written_before_the_generation() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert connection.seen.index("detected_intents") < connection.seen.index("generations")


# The customer is owed an answer: a classifier that could not run is a gap in the evidence, not a turn that fails.
def test_a_classifier_that_cannot_run_does_not_stop_the_turn(caplog: pytest.LogCaptureFixture) -> None:
    connection = FakeConnection(tables())

    from adapters.llm import double as double_module

    original = double_module.DoubleClient.complete

    async def refusing(self, **kwargs: object):
        if kwargs.get("system") and double_module.INTENT_MARKER in str(kwargs["system"]):
            raise LLMUnavailable("double: the classifier is down")
        return await original(self, **kwargs)

    double_module.DoubleClient.complete = refusing
    try:
        with caplog.at_level(logging.WARNING, logger="sonia.api.turns"):
            with client(connection, doubled()) as http:
                response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})
    finally:
        double_module.DoubleClient.complete = original

    payload = response.json()
    assert payload["outcome"] == "generated", "a classifier that fell over took the customer's answer with it"
    assert "intents not classified" in caplog.text, "the gap in the evidence was not reported"
    # The DELETE still runs, so a retry of a batch that had intents does not keep the old ones.
    assert connection.seen.count("detected_intents") == 1
    assert "[LO QUE PIDIÓ EL CLIENTE, EN ORDEN]" not in payload["completion_text"]


# The pipeline the brain actually ships: the blocklist runs on every turn and leaves its row even when it refuses nothing, which
# is what makes the trace say a step ran and passed rather than say nothing at all (ADR-0016).
def test_a_turn_the_pipeline_let_through_still_leaves_its_trace() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.json()["outcome"] == "generated"
    # One DELETE and one INSERT per step of the two legs, in the transaction that holds the rest of the evidence of the turn.
    assert connection.seen.count("pipeline_steps") == 1 + len(runner.INBOUND) + len(runner.OUTBOUND)


# ADR-0006 end to end, which is the assertion that makes the decision verifiable instead of declared: the redaction happens in a
# step, the route hands the model what the leg returned, and the double echoes what it was handed — so the answer itself is the
# evidence of what the provider saw. And prompt_rendered has to carry the redacted version or the audit trail becomes the largest
# repository of personal data in the system.
def test_the_document_number_a_customer_wrote_never_reaches_the_provider() -> None:
    connection = FakeConnection(tables(messages=[message(f"hola, mi DNI es 12.345.678 y quiero alquilar")]))

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    payload = response.json()
    assert payload["outcome"] == "generated"
    assert "12.345.678" not in payload["completion_text"], "the datum ADR-0006 exists to stop reached the model"
    assert "[DNI]" in payload["completion_text"], "the model got the message with the datum gone and no marker in its place"
    rendered = [params["prompt_rendered"] for table, params in connection.sent if table == "generations"]
    assert rendered and "12.345.678" not in rendered[0], "prompt_rendered kept the original text (ADR-0006)"


# Section 4.2, the other cut: the answer was generated and it is not going to be sent. Nothing the customer wrote can produce it
# —the inbound guardrail would have redacted that— so the answer itself has to carry the datum, which is what a prompt component
# holding the agency own account does in production.
def test_an_answer_that_carried_a_personal_datum_is_generated_and_not_sent() -> None:
    connection = FakeConnection(tables())

    from adapters.llm import double as double_module

    original = double_module.DoubleClient.complete

    async def leaking(self, **kwargs: object):
        system = str(kwargs.get("system") or "")
        if double_module.INTENT_MARKER in system or double_module.TRIAGE_MARKER in system:
            return await original(self, **kwargs)
        return Completion(
            text=f"para la seña transferí al CBU {CBU}",
            model=str(kwargs["model"]),
            prompt_tokens=9,
            completion_tokens=7,
        )

    double_module.DoubleClient.complete = leaking
    try:
        with client(connection, doubled()) as http:
            response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})
    finally:
        double_module.DoubleClient.complete = original

    payload = response.json()
    assert payload["outcome"] == "blocked"
    # With the id and without the text: the core has to be able to point at the evidence and must not be able to send it.
    assert payload["generation_id"] is not None
    assert payload["completion_text"] is None, "a refused answer travelled back in a shape the core could send"
    written = [params for table, params in connection.sent if table == "generations"]
    assert len(written) == 1
    # Written once and already with its verdict, which is why GenerationStatus needed a sixth value at all (section 4.2).
    assert written[0]["status"] == "blocked", "a refused answer was recorded as one that went out"
    # And kept whole, components included: the row exists so somebody can see what was stopped and with what prompt.
    assert CBU in written[0]["completion_text"], "the answer that was stopped was not kept, so nobody can read what it said"
    assert connection.seen.count("generation_components") == 2


# position is the order inside the turn and not inside the leg, which is what lets the trace of a turn be read in order at all.
def test_the_positions_of_the_trace_run_across_the_two_legs() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    steps = [params for table, params in connection.sent if table == "pipeline_steps" and "position" in params]
    assert [p["position"] for p in steps] == [1, 2, 3, 4, 5], "the outbound leg started counting again"
    assert [p["direction"] for p in steps] == ["inbound", "inbound", "inbound", "outbound", "outbound"]


# Section 2, and it is the rule somebody will want to "fix" the first day it costs a generation: the verdict is written once and
# with the row, so a brain that falls over during the outbound leg loses that generation and leaves only the steps that ran.
def test_a_step_that_could_not_decide_the_answer_loses_the_generation(monkeypatch: pytest.MonkeyPatch) -> None:
    async def falling(request: StepRequest) -> StepResult:
        raise LLMUnavailable("double: no answer about the answer")

    monkeypatch.setattr(
        runner,
        "OUTBOUND",
        (Step(step=PipelineStep.GUARDRAIL, direction=MessageDirection.OUTBOUND, run=falling),),
    )
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.json()["outcome"] == "failed"
    assert "generations" not in connection.seen, "an answer nobody managed to check was written anyway"
    # The steps of the leg that did run are kept, and the intents of this turn go with the generation: what a failed turn leaves
    # behind is its trace and nothing else (section 5).
    assert connection.seen.count("pipeline_steps") == 1 + len(runner.INBOUND)
    assert "detected_intents" not in connection.seen


# The cut of 07-secuencia section 4.1: no generation, no message, and the trace is the only evidence the turn leaves behind.
def test_a_step_that_cuts_the_turn_answers_blocked_and_leaves_its_trace(monkeypatch: pytest.MonkeyPatch) -> None:
    async def blocking(request: StepRequest) -> StepResult:
        return StepResult(outcome="blocked", version="1", text=request.text, halts=True)

    monkeypatch.setattr(
        runner,
        "INBOUND",
        (Step(step=PipelineStep.BLOCKLIST, direction=MessageDirection.INBOUND, run=blocking),),
    )
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    payload = response.json()
    assert payload["outcome"] == "blocked"
    assert payload["generation_id"] is None, "a turn that never generated pointed at evidence that does not exist"
    assert payload["completion_text"] is None
    # One DELETE and one INSERT: without these rows, nothing says why this customer was not answered.
    assert connection.seen.count("pipeline_steps") == 2
    assert "generations" not in connection.seen
    # The cut comes before the classifier, so a blocked message does not pay for a model that nobody was going to answer with.
    assert "detected_intents" not in connection.seen


# Section 5 again, one step earlier than the generation: a provider that fell over mid-pipeline is a failed turn whose partial
# evidence is kept, because it is the record of what did manage to happen.
def test_a_step_that_could_not_decide_fails_the_turn_and_keeps_what_ran(monkeypatch: pytest.MonkeyPatch) -> None:
    async def passing(request: StepRequest) -> StepResult:
        return StepResult(outcome="pass", version="1", text=request.text)

    async def falling(request: StepRequest) -> StepResult:
        raise LLMUnavailable("double: the triage is down")

    monkeypatch.setattr(
        runner,
        "INBOUND",
        (
            Step(step=PipelineStep.BLOCKLIST, direction=MessageDirection.INBOUND, run=passing),
            Step(step=PipelineStep.TRIAGE, direction=MessageDirection.INBOUND, run=falling),
        ),
    )
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.json()["outcome"] == "failed"
    # The DELETE plus the one step that did decide: the one that fell over leaves no row, because no declared outcome domain
    # has a value for "could not decide" (ADR-0016).
    assert connection.seen.count("pipeline_steps") == 2
    assert "generations" not in connection.seen
