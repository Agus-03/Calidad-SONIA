# The signals of F12 through POST /turns: what is written whether it fired or not, what cuts, and the mode OE6 reads (ADR-0045).

from decimal import Decimal

from pipeline.repository.turn import read_turn
from pipeline.repository.signals import read_no_resolution_streak
from tests.turns.doubles import (
    AGENT_ID,
    BATCH_ID,
    CONVERSATION_ID,
    FakeConnection,
    body,
    client,
    doubled,
    message,
    policy,
    tables,
)


def written_signals(connection: FakeConnection) -> dict[str, dict]:
    return {p["signal_type"]: p for t, p in connection.sent if t == "signal_evaluations" and "score" in p}


def triage_row(connection: FakeConnection) -> dict:
    return next(p for t, p in connection.sent if t == "pipeline_steps" and p.get("step") == "triage")


# ADR-0014 on the happy path too: a turn that answers still writes the signals it judged, and says which it skipped.
def test_a_turn_that_answers_still_writes_the_signals_it_judged() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.json()["outcome"] == "generated"
    signals = written_signals(connection)
    assert set(signals) == {"out_of_domain", "explicit_request"}, "the double shows no answer, so no_resolution is not judged"
    assert all(p["fired"] is False for p in signals.values())
    detail = triage_row(connection)["detail_json"].obj
    assert detail["skipped_signals"] == ["frustration", "no_resolution"]
    # ADR-0046, section 2.4: the mode is on the row OE6 reads its denominator from.
    assert detail["mode"] == "autonomous"
    assert response.json()["transfer"] is None


# ADR-0045, section 2.5: a request for a person the policy allows cuts the turn at the triage, with nothing said and no generation.
def test_a_request_for_a_person_cuts_the_turn_and_proposes_the_transfer() -> None:
    connection = FakeConnection(
        tables(
            messages=[message("hola, quiero hablar con una persona")],
            transfer_policies=[policy("explicit_request")],
        )
    )

    with client(connection, doubled()) as http:
        payload = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"}).json()

    assert payload["outcome"] == "blocked"
    assert payload["notice_text"] is None, "the customer asked for a person and got a text of ours"
    assert payload["generation_id"] is None, "a generation was paid for an answer nobody was going to send"
    signals = written_signals(connection)
    assert signals["explicit_request"]["fired"] is True
    assert payload["transfer"] == {"proposed": True, "signal_evaluation_id": str(signals["explicit_request"]["id"])}
    assert triage_row(connection)["detail_json"].obj["transfer"] == "explicit_request"
    assert "generations" not in connection.seen


# With no policy the same request does not fire, and the turn goes on and answers (ADR-0045, section 2.5).
def test_a_request_for_a_person_with_no_policy_is_answered() -> None:
    connection = FakeConnection(tables(messages=[message("quiero hablar con una persona")]))

    with client(connection, doubled()) as http:
        payload = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"}).json()

    assert payload["outcome"] == "generated"
    assert written_signals(connection)["explicit_request"]["score"] == Decimal(1)
    assert written_signals(connection)["explicit_request"]["fired"] is False


# ADR-0046, section 2.2: in suggestion mode the generation is a draft for the Operator and never an answer.
def test_in_suggestion_mode_the_generation_waits_for_review() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        payload = http.post("/turns", json=body(mode="suggestion"), headers={"X-Internal-Token": "test-token"}).json()

    written = next(p for t, p in connection.sent if t == "generations" and "prompt_rendered" in p)
    assert written["status"] == "pending_review"
    assert payload["outcome"] == "generated"
    assert triage_row(connection)["detail_json"].obj["mode"] == "suggestion"


# The run of no_resolution continues from the batch before (ADR-0045, section 2.2).
async def test_the_run_is_read_from_the_batch_before() -> None:
    turn = await read_turn(FakeConnection(tables()), BATCH_ID, CONVERSATION_ID, AGENT_ID)

    assert await read_no_resolution_streak(FakeConnection({"signal_evaluations": [{"score": Decimal(2)}]}), turn, "2") == 2
    assert await read_no_resolution_streak(FakeConnection({"signal_evaluations": []}), turn, "2") == 0
