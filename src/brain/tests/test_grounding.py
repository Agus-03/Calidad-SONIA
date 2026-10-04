# ADR-0068, reglas 2 to 7, and ADR-0074: the text the retrieval searches with, the row it leaves with every chunk it found, and
# the grounding's verdict, the chunks that reach the generation and the out_of_domain row a not_grounded writes.

from collections.abc import Sequence
from decimal import Decimal
from uuid import UUID, uuid4

import pytest

from infra.enums import MessageDirection, PipelineStep, TransferSignalType
from knowledge.search import Found, SearchResult, SearchRules
from pipeline import grounding, retrieval
from pipeline.repository.evidence import RetrievedChunk
from pipeline.repository.signals import TransferPolicy

RULES = SearchRules(version=1, top_k=3, threshold=Decimal("0.5"), max_query_chars=40)
DOCUMENT = uuid4()


class FakeSearcher:
    rules = RULES

    def __init__(self, result: SearchResult | None) -> None:
        self.result = result
        self.asked: list[tuple[UUID, str]] = []

    async def search(self, agent_id: UUID, text: str) -> SearchResult | None:
        self.asked.append((agent_id, text))
        return self.result


def found(*similarities: float) -> tuple[Found, ...]:
    return tuple(Found(uuid4(), DOCUMENT, f"trozo {rank}", s, rank) for rank, s in enumerate(similarities, start=1))


def test_the_question_is_the_summary_then_the_block_and_keeps_its_end_past_the_limit() -> None:
    assert retrieval.query("Busca depto en Jesus Maria.", "¿Y cuanto sale?", max_chars=100) == (
        "Busca depto en Jesus Maria.\n\n¿Y cuanto sale?"
    )
    assert retrieval.query(None, "  ¿Y cuanto sale?  ", max_chars=100) == "¿Y cuanto sale?", "the first turn has no summary"
    assert retrieval.query("   ", "hola", max_chars=100) == "hola"
    # The summary is cut from its start first, so the customer's own words are the last thing to go.
    assert retrieval.query("x" * 50, "¿Y cuanto sale?", max_chars=20) == "xxx\n\n¿Y cuanto sale?"
    assert retrieval.query(None, "y" * 50, max_chars=20) == "y" * 20


async def test_an_agent_that_does_not_retrieve_leaves_no_row() -> None:
    assert await retrieval.run(FakeSearcher(None), agent_id=uuid4(), summary=None, block="hola", position=4) is None


async def test_the_retrieval_row_keeps_every_chunk_its_parameters_and_what_was_left_out() -> None:
    agent = uuid4()
    chunks = found(0.81234567, 0.3)
    searcher = FakeSearcher(SearchResult(found=chunks, stale_document_ids=(uuid4(),), dropped=1))

    result = await retrieval.run(searcher, agent_id=agent, summary="Busca depto.", block="¿Cuanto sale?", position=4)

    assert searcher.asked == [(agent, "Busca depto.\n\n¿Cuanto sale?")]
    row = result.row
    assert (row.step, row.direction, row.position, row.outcome, row.version) == (
        PipelineStep.RETRIEVAL,
        MessageDirection.INBOUND,
        4,
        "hit",
        "1",
    )
    assert row.score is None, "only triage and grounding produce a score"
    assert row.detail == {
        "top_k": 3,
        "threshold": "0.5",
        "stale_documents": 1,
        "dropped": 1,
        "query_chars": 27,
        "truncated": False,
    }
    assert row.retrieved == (
        RetrievedChunk(chunks[0].chunk_id, Decimal("0.812346"), 1),
        RetrievedChunk(chunks[1].chunk_id, Decimal("0.300000"), 2),
    ), "every chunk, under the threshold too: that is what lets the threshold be swept later"
    assert result.found == chunks


async def test_the_row_says_how_long_the_searched_text_was_and_that_the_limit_cut_it() -> None:
    searcher = FakeSearcher(SearchResult(found=()))

    result = await retrieval.run(searcher, agent_id=uuid4(), summary="x" * 50, block="¿Y cuanto sale?", position=4)

    assert searcher.asked[0][1] == ("x" * 50 + "\n\n¿Y cuanto sale?")[-40:]
    assert (result.row.detail["query_chars"], result.row.detail["truncated"]) == (40, True)


async def test_a_search_that_found_nothing_is_empty() -> None:
    result = await retrieval.run(FakeSearcher(SearchResult(found=())), agent_id=uuid4(), summary=None, block="x", position=4)
    assert result.row.outcome == "empty" and result.row.retrieved == ()


async def test_only_the_chunks_that_reach_the_threshold_go_to_the_generation() -> None:
    result = await retrieval.run(FakeSearcher(SearchResult(found=found(0.8, 0.5, 0.49))), agent_id=uuid4(), summary=None, block="x", position=4)

    verdict = grounding.run(result, rules=RULES, position=5)

    assert verdict.grounded and verdict.best == Decimal("0.800000")
    assert verdict.knowledge == ("trozo 1", "trozo 2"), "the threshold is reached at equal, and 0.49 does not"
    assert (verdict.row.step, verdict.row.position, verdict.row.outcome, verdict.row.score) == (
        PipelineStep.GROUNDING,
        5,
        "grounded",
        Decimal("0.800000"),
    )
    assert verdict.row.detail == {"threshold": "0.5", "passed": 2}


@pytest.mark.parametrize(("similarities", "best"), [((0.3, 0.1), Decimal("0.300000")), ((), None)])
async def test_under_the_threshold_or_with_nothing_found_it_is_not_grounded(
    similarities: Sequence[float], best: Decimal | None
) -> None:
    result = await retrieval.run(FakeSearcher(SearchResult(found=found(*similarities))), agent_id=uuid4(), summary=None, block="x", position=4)

    verdict = grounding.run(result, rules=RULES, position=5)

    assert not verdict.grounded and verdict.knowledge == ()
    assert verdict.row.outcome == "not_grounded" and verdict.row.score == best


async def test_not_grounded_writes_out_of_domain_turned_around_under_the_retrievals_classifier() -> None:
    result = await retrieval.run(FakeSearcher(SearchResult(found=found(0.3))), agent_id=uuid4(), summary=None, block="x", position=4)
    verdict = grounding.run(result, rules=RULES, position=5)
    batch = uuid4()

    on = grounding.signal(verdict, message_batch_id=batch, rules=RULES, policy=TransferPolicy(True, Decimal(1)))
    off = grounding.signal(verdict, message_batch_id=batch, rules=RULES, policy=TransferPolicy(False, Decimal(1)))
    none = grounding.signal(verdict, message_batch_id=batch, rules=RULES, policy=None)

    assert on.signal_type == TransferSignalType.OUT_OF_DOMAIN and on.message_batch_id == batch
    assert (on.score, on.threshold_used) == (Decimal("0.700000"), Decimal("0.5")), "higher is further from the documents"
    assert on.classifier_version == "retrieval1", "told apart from the row the triage wrote for the same signal"
    assert (on.fired, off.fired, none.fired) == (True, False, False), "the policy of out_of_domain decides the transfer"


async def test_nothing_found_scores_one_and_a_grounded_turn_never_fires() -> None:
    empty = await retrieval.run(FakeSearcher(SearchResult(found=())), agent_id=uuid4(), summary=None, block="x", position=4)
    nothing = grounding.signal(grounding.run(empty, rules=RULES, position=5), message_batch_id=uuid4(), rules=RULES, policy=None)
    assert nothing.score == Decimal(1)

    # Exactly at the threshold is grounded: its turned-around score equals the turned-around threshold and still does not fire.
    edge = await retrieval.run(FakeSearcher(SearchResult(found=found(0.5))), agent_id=uuid4(), summary=None, block="x", position=4)
    verdict = grounding.run(edge, rules=RULES, position=5)
    on = grounding.signal(verdict, message_batch_id=uuid4(), rules=RULES, policy=TransferPolicy(True, Decimal(1)))
    assert verdict.grounded and on.score == on.threshold_used and on.fired is False
