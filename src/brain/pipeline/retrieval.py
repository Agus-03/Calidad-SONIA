# The retrieval step of ADR-0016, the one with a textual anchor in F06 ("búsqueda semántica para enriquecer respuestas"): it searches
# the agent's knowledge base with the summary of the context and the redacted block, and keeps every chunk it found, with no
# threshold, so the threshold can be swept later with what is already in retrieval_logs (ADR-0068, reglas 2, 3 and 6; H-23).
#
# Not a Step of the runner (ADR-0074): it needs the searcher and the summary, which a StepRequest does not carry, and an agent
# that does not retrieve leaves no row at all (ADR-0068, regla 10), which a step of the runner cannot do.

from dataclasses import dataclass
from decimal import Decimal
from time import perf_counter
from uuid import UUID

from infra.enums import MessageDirection, PipelineStep
from knowledge.search import Found, Searcher
from pipeline.repository.evidence import RetrievedChunk, TracedStep

# The outcome domain declared for this step in DER/entidades.md.
HIT = "hit"
EMPTY = "empty"

# Six decimals of a cosine: more than any threshold will ever be compared at, and a stable number in the column and the log.
_SCALE = Decimal("0.000001")


@dataclass(frozen=True)
class Retrieval:
    row: TracedStep
    # What the grounding decides about, in rank order.
    found: tuple[Found, ...]


# ADR-0068, regla 2: the summary of the current context first and the block after it, and no model asked to rephrase. Past the
# limit the text keeps its end, which cuts the summary from its start first and the block only after, so what was searched is
# what the row says and the provider never truncates in silence (ADR-0074).
def query(summary: str | None, block: str, *, max_chars: int) -> str:
    return _joined(summary, block)[-max_chars:]


def _joined(summary: str | None, block: str) -> str:
    return f"{summary.strip()}\n\n{block.strip()}" if summary and summary.strip() else block.strip()


# None when the agent does not retrieve (ADR-0068, regla 10): no row, and the grounding does not run either. The provider's and
# Qdrant's failures are raised to the route, which fails the turn as it does when the triage's provider fails (ADR-0074, regla 2).
async def run(searcher: Searcher, *, agent_id: UUID, summary: str | None, block: str, position: int) -> Retrieval | None:
    rules = searcher.rules
    text = query(summary, block, max_chars=rules.max_query_chars)
    started = perf_counter()
    result = await searcher.search(agent_id, text)
    latency_ms = round((perf_counter() - started) * 1000)
    if result is None:
        return None

    row = TracedStep(
        step=PipelineStep.RETRIEVAL,
        direction=MessageDirection.INBOUND,
        position=position,
        outcome=HIT if result.found else EMPTY,
        version=str(rules.version),
        latency_ms=latency_ms,
        # The two parameters ADR-0016 §2 asks for, and what the search left out, so a measurement can tell "the documents do not
        # answer" from "the search could not look" (ADR-0073, nota). Counts and never ids: the ids are in the log of the search.
        # And how long the searched text was and whether the limit cut it, never the text: it carries what the customer wrote.
        detail={
            "top_k": rules.top_k,
            "threshold": str(rules.threshold),
            "stale_documents": len(result.stale_document_ids),
            "dropped": result.dropped,
            "query_chars": len(text),
            "truncated": len(_joined(summary, block)) > rules.max_query_chars,
        },
        retrieved=tuple(
            RetrievedChunk(document_chunk_id=f.chunk_id, similarity=similarity(f), rank=f.rank) for f in result.found
        ),
    )
    return Retrieval(row=row, found=result.found)


def similarity(found: Found) -> Decimal:
    return Decimal(str(found.similarity)).quantize(_SCALE)
