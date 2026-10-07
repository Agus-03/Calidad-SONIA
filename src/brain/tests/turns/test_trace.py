# The trace of ADR-0016 through POST /turns: the rows each leg leaves, their positions, and every place a turn can be cut.

from decimal import Decimal

import pytest

from adapters.llm.base import Completion, LLMUnavailable
from infra.enums import MessageDirection, PipelineStep
from pipeline import notices, runner
from pipeline.step import Step, StepRequest, StepResult
from tests.turns.doubles import FakeConnection, body, client, doubled, message, policy, tables


# A real CBU, because the outbound guardrail is only worth testing against what it is meant to catch.
CBU = "0170099220000067797151"

# The pipeline the brain actually ships: the blocklist runs on every turn and leaves its row even when it refuses nothing, which
# is what makes the trace say a step ran and passed rather than say nothing at all (ADR-0016).
def test_a_turn_the_pipeline_let_through_still_leaves_its_trace() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.json()["outcome"] == "generated"
    # One DELETE and one INSERT per step of the two legs, plus the generation between them, in the transaction that holds the rest
    # of the evidence of the turn.
    assert connection.seen.count("pipeline_steps") == 1 + len(runner.INBOUND) + 1 + len(runner.OUTBOUND)


# The sixth step of ADR-0016, which is the one row of the trace that mostly points at another table: the tokens, the cost and the
# text are in generations, and what this row adds is that the generation happened, when, and in what order (H-23).
def test_the_generation_leaves_its_own_row_pointing_at_the_answer_it_produced() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    steps = [params for table, params in connection.sent if table == "pipeline_steps" and "position" in params]
    row = next(p for p in steps if p["step"] == "generation")
    written = [params for table, params in connection.sent if table == "generations" and "prompt_rendered" in params]

    assert row["outcome"] == "ok"
    # outbound, the same reading the second run of the blocklist and the guardrail get: what it produced is the answer.
    assert row["direction"] == "outbound"
    assert row["score"] is None, "a step that produces no confidence wrote one"
    # The pointer to the row that holds the rest, which is what makes the trace navigable instead of duplicated.
    assert row["detail_json"].obj == {"generation_id": str(written[0]["id"])}
    # Measured by generate() and not by the route, so the trace and generations do not disagree about the same provider call.
    assert row["latency_ms"] == written[0]["latency_ms"]


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
    # And the customer is not left talking to nobody: the fixed text of this cut point travels instead of the answer (O2).
    assert payload["notice_text"] == notices.rules().text_for(PipelineStep.GUARDRAIL, MessageDirection.OUTBOUND)
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
    assert [p["position"] for p in steps] == [1, 2, 3, 4, 5, 6], "the outbound leg started counting again"
    assert [p["direction"] for p in steps] == ["inbound", "inbound", "inbound", "outbound", "outbound", "outbound"]
    # The generation is the fourth thing that happened, between the leg that decided about the question and the one that decides
    # about the answer: that position is what makes the whole turn readable in order (ADR-0016).
    assert [p["step"] for p in steps] == ["blocklist", "guardrail", "triage", "generation", "blocklist", "guardrail"]


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
    # The steps of the leg that ran plus the generation that did happen: what is lost is the generation row of the other table,
    # which is written once and already with its verdict (section 2).
    assert connection.seen.count("pipeline_steps") == 1 + len(runner.INBOUND) + 1
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
    # The one part of section 4.1 that O2 amends: there is no generation and there is an answer, and it is the text of the file.
    assert payload["notice_text"] == notices.rules().text_for(PipelineStep.BLOCKLIST, MessageDirection.INBOUND)
    # One DELETE and one INSERT: without these rows, nothing says why this customer was not answered.
    assert connection.seen.count("pipeline_steps") == 2
    assert "generations" not in connection.seen
    # The cut comes before the classifier, so a blocked message does not pay for a model that nobody was going to answer with.
    assert "detected_intents" not in connection.seen


# Decision O1c, end to end: a question outside the domain is answered by nobody. The turn is cut like any other, the trace says so,
# and what travels back has no text at all — the core writes no message, drops the conversation to waiting and a person picks it up.
def test_a_question_outside_the_domain_is_answered_by_a_person_and_never_by_a_text(monkeypatch: pytest.MonkeyPatch) -> None:
    async def outside(request: StepRequest) -> StepResult:
        return StepResult(outcome="out_of_domain", version="1", text=request.text, halts=True, score=Decimal("0.91"))

    monkeypatch.setattr(
        runner,
        "INBOUND",
        (Step(step=PipelineStep.TRIAGE, direction=MessageDirection.INBOUND, run=outside),),
    )
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    payload = response.json()
    assert payload["outcome"] == "blocked"
    assert payload["notice_text"] is None, "the customer was told something about a question O1c answers with a person"
    assert payload["completion_text"] is None
    assert payload["generation_id"] is None
    # The cut is evidence like any other: the row that says out_of_domain is what OE6 counts on (ADR-0024).
    steps = [params for table, params in connection.sent if table == "pipeline_steps" and "outcome" in params]
    assert [p["outcome"] for p in steps] == ["out_of_domain"]
    # And the signal of F12 is written even though this agent has no policy for it, because a detector whose precision is an
    # objective persists its output whether it fired or not (OE4). proposed is false and the id still names the evidence.
    signals = [params for table, params in connection.sent if table == "signal_evaluations" and "score" in params]
    assert len(signals) == 1
    assert signals[0]["signal_type"] == "out_of_domain"
    # A yes/no signal is 1 against 1 (ADR-0045, section 2.4); the confidence stays in the row of the step.
    assert signals[0]["score"] == Decimal(1)
    assert signals[0]["threshold_used"] == Decimal(1), "a threshold nothing was compared against went into the row"
    assert signals[0]["fired"] is False, "a transfer fired for an agent whose administrator never configured this signal"
    assert payload["transfer"] == {"proposed": False, "signal_evaluation_id": str(signals[0]["id"])}


# The other half of O1c, with the policy the administrator configured: the same cut now proposes the transfer, and the core is the
# one that writes transfer_events — the brain proposes and never acts (07-secuencia 0).
def test_a_question_outside_the_domain_proposes_the_transfer_the_policy_allows(monkeypatch: pytest.MonkeyPatch) -> None:
    async def outside(request: StepRequest) -> StepResult:
        return StepResult(outcome="out_of_domain", version="3", text=request.text, halts=True, score=Decimal("0.88"))

    monkeypatch.setattr(
        runner,
        "INBOUND",
        (Step(step=PipelineStep.TRIAGE, direction=MessageDirection.INBOUND, run=outside),),
    )
    connection = FakeConnection(tables(transfer_policies=[policy("out_of_domain")]))

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    payload = response.json()
    signals = [params for table, params in connection.sent if table == "signal_evaluations" and "score" in params]
    assert len(signals) == 1
    assert signals[0]["fired"] is True
    assert payload["transfer"] == {"proposed": True, "signal_evaluation_id": str(signals[0]["id"])}
    assert signals[0]["threshold_used"] == Decimal(1)
    # The file that decided, which is what classifier_version has to name (regla 9).
    assert signals[0]["classifier_version"] == "3"
    # Still no text: a policy that allows the transfer does not turn O1c into an answer.
    assert payload["notice_text"] is None


# Only the triage delegates. A message with a forbidden term is not a question somebody else can answer better, and O2 already
# answers it with a text: writing a signal for it would count an escalation that nobody decided.
def test_a_cut_that_is_not_the_triage_proposes_no_transfer(monkeypatch: pytest.MonkeyPatch) -> None:
    async def blocking(request: StepRequest) -> StepResult:
        return StepResult(outcome="blocked", version="1", text=request.text, halts=True)

    monkeypatch.setattr(
        runner,
        "INBOUND",
        (Step(step=PipelineStep.BLOCKLIST, direction=MessageDirection.INBOUND, run=blocking),),
    )
    connection = FakeConnection(tables(transfer_policies=[policy("out_of_domain")]))

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.json()["transfer"] is None
    assert "signal_evaluations" not in connection.seen, "a blocked term was counted as a question for a person"
    assert "transfer_policies" not in connection.seen, "a cut that never delegates read the policy anyway"
    # ADR-0045, section 2.6: the row that cut says which signals were never evaluated, since none has a row of its own.
    row = next(p for t, p in connection.sent if t == "pipeline_steps" and "position" in p)
    assert row["detail_json"].obj["skipped_signals"] == [
        "explicit_request",
        "frustration",
        "out_of_domain",
        "no_resolution",
    ]


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
