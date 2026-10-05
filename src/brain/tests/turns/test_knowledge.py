# F06 through POST /turns (ADR-0068, ADR-0074): the two steps after the triage, the chunks that reach the prompt, the cut of the
# grounding with its text, its signal and its transfer, and the turns that do not retrieve at all.

from decimal import Decimal
from uuid import UUID, uuid4

import pytest

from adapters.embeddings.base import EmbeddingError
from adapters.llm import double
from adapters.vectorstore.qdrant import VectorStoreError
from knowledge.search import Found, SearchResult, SearchRules
from tests.turns.doubles import FakeConnection, body, client, doubled, message, policy, tables

HEADERS = {"X-Internal-Token": "test-token"}
RULES = SearchRules(version=1, top_k=3, threshold=Decimal("0.5"), max_query_chars=4000)
DOCUMENT = uuid4()
PRICE = Found(uuid4(), DOCUMENT, "Alquiler del depto en Jesus Maria: $320.000 + expensas.", 0.81, 1)
ROOMS = Found(uuid4(), DOCUMENT, "El depto tiene 2 ambientes.", 0.42, 2)


class FakeSearcher:
    rules = RULES

    def __init__(self, result: SearchResult | None = None, error: Exception | None = None) -> None:
        self.result = result
        self.error = error
        self.asked: list[tuple[UUID, str]] = []

    async def search(self, agent_id: UUID, text: str) -> SearchResult | None:
        self.asked.append((agent_id, text))
        if self.error is not None:
            raise self.error
        return self.result


def turn(connection: FakeConnection, searcher: FakeSearcher) -> dict:
    with client(connection, doubled()) as http:
        http.app.state.searcher = searcher
        return http.post("/turns", json=body(), headers=HEADERS).json()


def steps(connection: FakeConnection) -> list[dict]:
    return [p for t, p in connection.sent if t == "pipeline_steps" and "step" in p]


def logs(connection: FakeConnection) -> list[dict]:
    return [p for t, p in connection.sent if t == "retrieval_logs" and "rank" in p]


def asking(text: str) -> FakeConnection:
    return FakeConnection(tables(messages=[message(text)], retrieval_logs=[]))


def test_a_grounded_turn_answers_with_the_chunks_that_reach_the_threshold() -> None:
    connection = asking("¿cuanto sale el alquiler?")
    searcher = FakeSearcher(SearchResult(found=(PRICE, ROOMS)))

    payload = turn(connection, searcher)

    assert payload["outcome"] == "generated"
    assert searcher.asked[0][1] == "¿cuanto sale el alquiler?", "the first turn has no summary to search with"
    trace = steps(connection)
    assert [s["step"] for s in trace[:5]] == ["blocklist", "guardrail", "triage", "retrieval", "grounding"]
    assert [s["position"] for s in trace] == list(range(1, len(trace) + 1)), "one numbering across the whole turn"
    retrieval_row, grounding_row = trace[3], trace[4]
    assert (retrieval_row["outcome"], retrieval_row["version"]) == ("hit", "1")
    assert retrieval_row["detail_json"].obj == {
        "top_k": 3,
        "threshold": "0.5",
        "stale_documents": 0,
        "dropped": 0,
        "query_chars": 25,
        "truncated": False,
    }
    assert (grounding_row["outcome"], grounding_row["score"]) == ("grounded", Decimal("0.810000"))
    assert [(log["document_chunk_id"], log["rank"]) for log in logs(connection)] == [(PRICE.chunk_id, 1), (ROOMS.chunk_id, 2)]
    assert {log["pipeline_step_id"] for log in logs(connection)} == {retrieval_row["id"]}, "they hang from the retrieval row"
    prompt = next(p for t, p in connection.sent if t == "generations" and "prompt_rendered" in p)["prompt_rendered"]
    assert PRICE.content in prompt and ROOMS.content not in prompt, "only what reaches the threshold is given to the model"


def test_a_question_the_documents_do_not_answer_is_told_and_transferred() -> None:
    connection = FakeConnection(
        tables(messages=[message("¿puedo ir a verlo el martes?")], retrieval_logs=[], transfer_policies=[policy("out_of_domain")])
    )

    payload = turn(connection, FakeSearcher(SearchResult(found=(ROOMS,))))

    assert payload["outcome"] == "blocked"
    assert payload["notice_text"], "unlike the triage's cut, the customer is told (ADR-0068, regla 8)"
    assert payload["generation_id"] is None and "generations" not in connection.seen
    written = [p for t, p in connection.sent if t == "signal_evaluations" and "score" in p]
    grounding_signal = next(p for p in written if p["classifier_version"] == "retrieval1")
    assert grounding_signal["signal_type"] == "out_of_domain"
    assert (grounding_signal["score"], grounding_signal["threshold_used"]) == (Decimal("0.580000"), Decimal("0.5"))
    assert grounding_signal["fired"] is True
    assert any(p["classifier_version"] != "retrieval1" for p in written if p["signal_type"] == "out_of_domain"), (
        "the triage's row for the same signal is still there"
    )
    assert payload["transfer"] == {"proposed": True, "signal_evaluation_id": str(grounding_signal["id"])}
    cut = steps(connection)[-1]
    assert (cut["step"], cut["outcome"]) == ("grounding", "not_grounded")
    assert cut["detail_json"].obj["transfer"] == "out_of_domain" and cut["detail_json"].obj["mode"] == "autonomous"
    assert len(logs(connection)) == 1, "what was retrieved is the evidence of the cut"


def test_without_a_policy_the_cut_is_told_and_not_transferred() -> None:
    connection = asking("¿puedo ir a verlo el martes?")

    payload = turn(connection, FakeSearcher(SearchResult(found=())))

    assert payload["outcome"] == "blocked" and payload["notice_text"]
    assert payload["transfer"]["proposed"] is False
    assert steps(connection)[3]["outcome"] == "empty"


def test_an_agent_that_does_not_retrieve_answers_as_before() -> None:
    connection = asking("¿cuanto sale?")

    payload = turn(connection, FakeSearcher(None))

    assert payload["outcome"] == "generated"
    assert "retrieval" not in [s["step"] for s in steps(connection)] and logs(connection) == []


def test_a_greeting_does_not_search(monkeypatch) -> None:
    monkeypatch.setattr(double, "TRIAGE_ANSWER", double.TRIAGE_ANSWER.replace('"greeting": false', '"greeting": true'))
    connection = asking("hola")
    searcher = FakeSearcher(SearchResult(found=()))

    payload = turn(connection, searcher)

    assert payload["outcome"] == "generated" and searcher.asked == [], "a greeting would come out not_grounded"


@pytest.mark.parametrize(
    "error",
    [VectorStoreError("qdrant: POST /collections/kb/points/query: 404"), EmbeddingError("voyage: TimeoutError: ")],
)
def test_a_search_that_fails_fails_the_turn_and_leaves_its_row_for_oe6(error: Exception) -> None:
    connection = asking("¿cuanto sale?")

    payload = turn(connection, FakeSearcher(error=error))

    assert payload["outcome"] == "failed"
    trace = steps(connection)
    assert [s["step"] for s in trace] == ["blocklist", "guardrail", "triage", "retrieval"]
    failed = trace[-1]
    assert (failed["outcome"], failed["position"]) == ("failed", 4), "OE6 reads the block as not answered (ADR-0077)"
    assert failed["detail_json"].obj["error"] == type(error).__name__
    assert str(error) not in str(failed["detail_json"].obj), "the message may carry what Qdrant answered"
    assert logs(connection) == [] and "generations" not in connection.seen


# In suggestion mode the brain cuts the same way and says the mode on the row: the core then sends no text and transfers nothing
# (ADR-0046), and the Operator gets no draft for this turn (ADR-0074).
def test_in_suggestion_mode_the_cut_is_the_same_and_the_row_says_the_mode() -> None:
    connection = FakeConnection(
        tables(messages=[message("¿puedo ir el martes?")], retrieval_logs=[], transfer_policies=[policy("out_of_domain")])
    )

    with client(connection, doubled()) as http:
        http.app.state.searcher = FakeSearcher(SearchResult(found=(ROOMS,)))
        payload = http.post("/turns", json=body(mode="suggestion"), headers=HEADERS).json()

    assert payload["outcome"] == "blocked" and payload["generation_id"] is None
    assert steps(connection)[-1]["detail_json"].obj["mode"] == "suggestion"
