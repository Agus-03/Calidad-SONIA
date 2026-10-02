# What the turn carries beside the answer: the context of F04, the extraction of F31 and the quick replies of F07.

import logging
from uuid import uuid4

import pytest

from adapters.llm.base import LLMUnavailable
from tests.turns.doubles import AGENT_ID, FakeConnection, body, client, doubled, message, policy, tables


# ADR-0048: the context of F04 as the turn reads it and leaves it. conversation_contexts is read and never written here: the core
# copies the version into it when it closes the turn.
VERSION_ID = uuid4()


def current_context(summary: str = "Venía preguntando por Nueva Córdoba.") -> dict:
    return {
        "turn_number": 1,
        "summary": summary,
        "primary_intent": "consulta_zona",
        "objections_json": [],
        "urgency_level": None,
        "budget_signal": None,
        "extracted_data_json": {},
    }


def sent_to(connection: FakeConnection, table: str, key: str) -> list[dict]:
    return [params for name, params in connection.sent if name == table and key in params]


def without_rewriter():
    from adapters.llm import double as double_module

    original = double_module.DoubleClient.complete

    async def down(self, **kwargs: object):
        if double_module.REWRITER_MARKER in str(kwargs.get("system") or ""):
            raise LLMUnavailable("double: the rewriter is down")
        return await original(self, **kwargs)

    return double_module, original, down


def test_a_generated_turn_leaves_its_context_version_and_the_generation_points_at_it() -> None:
    connection = FakeConnection(tables(conversation_contexts=[], conversation_context_versions=[]))

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    payload = response.json()
    assert payload["outcome"] == "generated"
    # The double echoes the envelope, so the answer shows the rewritten context reached the model under its heading (Regla 1).
    assert "[CONTEXTO DE LA CONVERSACIÓN]" in payload["completion_text"]
    assert "[doblado] el cliente consulta" in payload["completion_text"]
    version = sent_to(connection, "conversation_context_versions", "content")[0]
    generation = sent_to(connection, "generations", "prompt_rendered")[0]
    assert generation["context_version_id"] == version["id"], "the generation does not name the context it was given"
    # Before the generation, which has a foreign key to it.
    assert connection.seen.index("conversation_context_versions") < connection.seen.index("generations")
    assert version["turn_number"] == 1 and version["rewritten_by_model"] == "command-a-03-2025"
    # primary_intent is the dominant label of F11 and never the rewriter's (DER/entidades.md).
    assert version["content"].obj["primary_intent"] == "consulta_disponibilidad"


# Regla 3: a rewrite that fell over does not take the answer with it, and the generation is given the context there was.
def test_a_rewrite_that_fails_answers_with_the_context_there_was(caplog: pytest.LogCaptureFixture) -> None:
    connection = FakeConnection(
        tables(conversation_contexts=[current_context()], conversation_context_versions=[{"id": VERSION_ID}])
    )
    double_module, original, down = without_rewriter()

    double_module.DoubleClient.complete = down
    try:
        with caplog.at_level(logging.WARNING, logger="sonia.api.turns"):
            with client(connection, doubled()) as http:
                response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})
    finally:
        double_module.DoubleClient.complete = original

    payload = response.json()
    assert payload["outcome"] == "generated", "a rewrite that fell over took the customer's answer with it"
    assert "Venía preguntando por Nueva Córdoba." in payload["completion_text"]
    assert "context not rewritten" in caplog.text, "the gap in the evidence was not reported"
    assert sent_to(connection, "conversation_context_versions", "content") == [], "a version was written for no rewrite"
    assert sent_to(connection, "generations", "prompt_rendered")[0]["context_version_id"] == VERSION_ID


def test_a_first_turn_whose_rewrite_fails_answers_with_no_context() -> None:
    connection = FakeConnection(tables(conversation_contexts=[], conversation_context_versions=[]))
    double_module, original, down = without_rewriter()

    double_module.DoubleClient.complete = down
    try:
        with client(connection, doubled()) as http:
            response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})
    finally:
        double_module.DoubleClient.complete = original

    assert response.json()["outcome"] == "generated"
    assert "[CONTEXTO DE LA CONVERSACIÓN]" not in response.json()["completion_text"]
    assert sent_to(connection, "generations", "prompt_rendered")[0]["context_version_id"] is None


# A turn cut at the triage has no answer to give a context to, so it neither reads nor rewrites one (ADR-0048).
def test_a_turn_cut_at_the_triage_does_not_rewrite_the_context() -> None:
    connection = FakeConnection(
        tables(
            messages=[message("quiero hablar con una persona")],
            transfer_policies=[policy("explicit_request")],
            conversation_contexts=[current_context()],
            conversation_context_versions=[],
        )
    )

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    assert response.json()["outcome"] == "blocked"
    assert "conversation_contexts" not in connection.seen
    assert "conversation_context_versions" not in connection.seen


# ADR-0047: what the turn hands the core about the contact. The agent of these cases has the toggle on.
def extracting(**overrides: list[dict]) -> dict[str, list[dict]]:
    base = tables(**overrides)
    base["ai_agents"] = [{**base["ai_agents"][0], "extraction_enabled": True}]
    return base


def test_a_generated_turn_hands_the_core_what_the_customer_said_about_themselves() -> None:
    connection = FakeConnection(extracting(messages=[message("hola, me llamo Lucía Pérez y mi DNI es 30.123.456")]))

    with client(connection, doubled()) as http:
        payload = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"}).json()

    assert payload["outcome"] == "generated"
    got = {e["key"]: e for e in payload["extracted"]}
    # The DNI by the pattern of the checkpoint, in digits only; the name by the model, over the redacted text.
    assert got["dni"]["value"] == "30123456" and got["dni"]["confidence"] == 1.0
    assert got["first_name"]["value"] == "Lucía" and got["last_name"]["value"] == "Pérez"
    assert "30.123.456" not in payload["completion_text"], "the DNI reached the model the extraction runs beside"


def test_with_the_toggle_off_nothing_is_extracted_and_the_extractor_is_never_asked() -> None:
    connection = FakeConnection(tables(messages=[message("me llamo Lucía Pérez, DNI 30123456")]))

    from adapters.llm import double as double_module

    original = double_module.DoubleClient.complete
    asked: list[str] = []

    async def watching(self, **kwargs: object):
        asked.append(str(kwargs.get("system") or ""))
        return await original(self, **kwargs)

    double_module.DoubleClient.complete = watching
    try:
        with client(connection, doubled()) as http:
            payload = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"}).json()
    finally:
        double_module.DoubleClient.complete = original

    assert payload["extracted"] == []
    assert not [s for s in asked if double_module.EXTRACTION_MARKER in s], "the extractor was called with the toggle off"


# A request for a person often comes with the DNI: the checkpoint half runs on the turn the triage cut, the model half does not.
def test_a_turn_cut_at_the_triage_still_hands_over_what_the_checkpoint_found() -> None:
    connection = FakeConnection(
        extracting(
            messages=[message("quiero hablar con una persona, me llamo Lucía Pérez y mi DNI es 30123456")],
            transfer_policies=[policy("explicit_request")],
        )
    )

    with client(connection, doubled()) as http:
        payload = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"}).json()

    assert payload["outcome"] == "blocked"
    assert [e["key"] for e in payload["extracted"]] == ["dni"]


def test_an_extractor_that_falls_over_does_not_take_the_answer_with_it(caplog: pytest.LogCaptureFixture) -> None:
    connection = FakeConnection(extracting(messages=[message("me llamo Lucía Pérez, DNI 30123456")]))

    from adapters.llm import double as double_module

    original = double_module.DoubleClient.complete

    async def down(self, **kwargs: object):
        if double_module.EXTRACTION_MARKER in str(kwargs.get("system") or ""):
            raise LLMUnavailable("double: the extractor is down")
        return await original(self, **kwargs)

    double_module.DoubleClient.complete = down
    try:
        with caplog.at_level(logging.WARNING, logger="sonia.api.turns"):
            with client(connection, doubled()) as http:
                payload = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"}).json()
    finally:
        double_module.DoubleClient.complete = original

    assert payload["outcome"] == "generated"
    assert "contact data not extracted" in caplog.text
    # The checkpoint half does not depend on the model, so the DNI still travels.
    assert [e["key"] for e in payload["extracted"]] == ["dni"]


# F07 end to end: the catalogue of the agent's account is read on the turn and reaches the model that answers (ADR-0061).
def test_the_quick_replies_of_the_account_reach_the_model() -> None:
    connection = FakeConnection(tables(quick_replies=[{"title": "Horarios", "content": "Atendemos de 9 a 18."}]))

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})

    payload = response.json()
    assert payload["outcome"] == "generated"
    asked = next(params for table, params in connection.sent if table == "quick_replies")
    assert asked["agent"] == AGENT_ID
    # The double echoes the envelope, so the answer is what proves the catalogue reached the model and not only the read.
    assert "[RESPUESTAS RÁPIDAS DE LA EMPRESA]" in payload["completion_text"]
    assert "1. Horarios\nAtendemos de 9 a 18." in payload["completion_text"]


# No catalogue, no section: the default tables carry none, and the envelope has to say nothing about it.
def test_a_turn_with_no_quick_replies_prints_no_heading() -> None:
    connection = FakeConnection(tables())

    with client(connection, doubled()) as http:
        response = http.post("/turns", json=body(), headers={"X-Internal-Token": "test-token"})
