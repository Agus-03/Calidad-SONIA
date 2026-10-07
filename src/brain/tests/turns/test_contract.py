# The contract of 07-secuencia section 1, exercised without PostgreSQL: ADR-0028 asks for doubled dependencies from day one.

import logging

import pytest

from adapters.llm.base import LLMUnavailable
from infra.config import Settings
from pipeline import runner
from pipeline.repository.turn import TurnNotFound, read_turn
from tests.turns.doubles import (
    AGENT_ID,
    BATCH_ID,
    CONVERSATION_ID,
    FakeConnection,
    body,
    client,
    doubled,
    message,
    tables,
)



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
    assert set(payload) == {"outcome", "generation_id", "completion_text", "transfer", "notice_text", "extracted"}
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
    # What did run is kept, which is section 5: one DELETE, one row per step of the leg that decided, and the row of the generation
    # that did not happen. Without them this batch would read as one nothing ever ran on.
    assert connection.seen.count("pipeline_steps") == 1 + len(runner.INBOUND) + 1
    steps = [params for table, params in connection.sent if table == "pipeline_steps" and "position" in params]
    # The row that says where the turn died, and the only evidence it leaves: generations has none at all. The error is named by
    # its class and never by the provider's own text.
    assert steps[-1]["step"] == "generation"
    assert steps[-1]["outcome"] == "failed"
    assert steps[-1]["position"] == len(runner.INBOUND) + 1


# doubled is the whole of F11 against the doubled adapter: the classifier answers two intentions, they are written as evidence,
# and they reach the model that reads the message (ADR-0041, regla 12).
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
