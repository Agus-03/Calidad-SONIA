# ADR-0064, reglas 7 a 10: one document from pending to indexed, in the order of writing the rule declares, and every way it can
# end failed instead of stuck in processing. The repository is replaced by memory, so the order is what is being tested.

from contextlib import asynccontextmanager
from dataclasses import dataclass, field
from uuid import UUID, uuid4

import pytest

from adapters.embeddings.double import DoubleEmbedder
from adapters.llm.double import DoubleClient
from adapters.vectorstore.qdrant import Point
from knowledge import chunking
from tests.worker.pdfs import image_pdf, joined, text_pdf
from worker import repository
from worker.images import Describer, rules
from worker.ingest import Ingestor, Parts


@dataclass
class Memory:
    pending: list[repository.Claimed] = field(default_factory=list)
    chunks: dict[UUID, list[repository.Chunk]] = field(default_factory=dict)
    status: dict[UUID, tuple[str, str | None]] = field(default_factory=dict)
    removed: list[repository.Removed] = field(default_factory=list)
    stale: list[UUID] = field(default_factory=list)
    log: list[str] = field(default_factory=list)


class FakeConn:
    @asynccontextmanager
    async def transaction(self):
        yield


@pytest.fixture
def memory(monkeypatch: pytest.MonkeyPatch) -> Memory:
    mem = Memory()

    async def claim(conn: object) -> repository.Claimed | None:
        return mem.pending.pop(0) if mem.pending else None

    async def replace_chunks(conn: object, document_id: UUID, chunks: list[repository.Chunk]) -> None:
        mem.log.append("chunks")
        mem.chunks[document_id] = list(chunks)

    async def mark_indexed(conn: object, document_id: UUID) -> bool:
        mem.log.append("indexed")
        mem.status[document_id] = ("indexed", None)
        return True

    async def mark_failed(conn: object, document_id: UUID, reason: str) -> bool:
        mem.status[document_id] = ("failed", reason)
        return True

    async def release_stale(conn: object, *, timeout_seconds: float) -> list[UUID]:
        released, mem.stale = mem.stale, []
        return released

    async def removed_with_live_chunks(conn: object) -> list[repository.Removed]:
        found, mem.removed = mem.removed, []
        return found

    async def retire_chunks(conn: object, document_id: UUID) -> None:
        mem.log.append(f"retire {document_id}")

    for name, fn in {
        "claim": claim,
        "replace_chunks": replace_chunks,
        "mark_indexed": mark_indexed,
        "mark_failed": mark_failed,
        "release_stale": release_stale,
        "removed_with_live_chunks": removed_with_live_chunks,
        "retire_chunks": retire_chunks,
    }.items():
        monkeypatch.setattr(repository, name, fn)
    return mem


class FakeObjects:
    def __init__(self, objects: dict[str, bytes]) -> None:
        self.objects = objects

    async def download(self, key: str) -> bytes:
        return self.objects[key]


class Clear:
    # The cipher is tested on its own (test_crypto.py): here a "sealed" object is the PDF with a prefix.
    def open(self, sealed: bytes) -> bytes:
        assert sealed.startswith(b"sealed:")
        return sealed.removeprefix(b"sealed:")


class FakeVectors:
    def __init__(self, log: list[str]) -> None:
        self.log = log
        self.collections: set[str] = set()
        self.points: dict[str, list[Point]] = {}

    async def ensure_collection(self, name: str) -> None:
        self.collections.add(name)

    async def upsert(self, name: str, points: list[Point]) -> None:
        self.log.append("upsert")
        self.points.setdefault(name, []).extend(points)

    async def delete_document(self, name: str, document_id: UUID) -> None:
        self.log.append(f"delete {document_id}")
        self.points[name] = [p for p in self.points.get(name, []) if p.document_id != document_id]


def build(memory: Memory, objects: dict[str, bytes], *, describer_client: object | None = None) -> tuple[Ingestor, FakeVectors]:
    vectors = FakeVectors(memory.log)

    @asynccontextmanager
    async def connection():
        yield FakeConn()

    parts = Parts(
        objects=FakeObjects(objects),
        opener=Clear(),
        embedder=DoubleEmbedder(),
        vectors=vectors,
        describer=Describer(describer_client or DoubleClient(), rules()),
        description_rules=rules(),
        chunking_rules=chunking.rules(),
        connection=connection,
    )
    return Ingestor(parts), vectors


def claimed() -> repository.Claimed:
    document = uuid4()
    return repository.Claimed(id=document, object_key=f"knowledge-bases/kb/{document}", collection_name="kb_agente")


async def test_a_document_goes_from_pending_to_indexed_in_the_order_of_the_rule(memory: Memory) -> None:
    doc = claimed()
    memory.pending.append(doc)
    pdf = joined(text_pdf(["Depto 2 ambientes en Jesus Maria. $320.000 mas expensas."]), image_pdf(400, 300))
    ingestor, vectors = build(memory, {doc.object_key: b"sealed:" + pdf})

    assert await ingestor.next() is True
    assert await ingestor.next() is False, "with nothing pending, next() says so and the loop sleeps"

    assert memory.status[doc.id] == ("indexed", None)
    # (i) the old vectors, (ii) the chunks, (iii) the new vectors, (iv) the status (ADR-0064, regla 7).
    assert memory.log == [f"delete {doc.id}", "chunks", "upsert", "indexed"]
    chunks = memory.chunks[doc.id]
    assert chunks and [c.index for c in chunks] == list(range(len(chunks)))
    text = "\n".join(c.content for c in chunks)
    assert "Jesus Maria" in text and "[Imagen: [doblado]" in text, "the description enters the text of its page"
    first = chunks[0]
    assert first.chunking_version == chunking.rules().version
    assert first.description_prompt_version == rules().version
    assert first.embedding_model == "double"
    assert first.token_count == chunking.rules().estimate_tokens(first.content)
    assert [p.id for p in vectors.points["kb_agente"]] == [c.id for c in chunks]
    assert "kb_agente" in vectors.collections


async def test_a_document_without_images_records_no_description_prompt(memory: Memory) -> None:
    doc = claimed()
    memory.pending.append(doc)
    ingestor, _ = build(memory, {doc.object_key: b"sealed:" + text_pdf(["Solo texto"])})

    await ingestor.next()

    assert all(c.description_prompt_version is None for c in memory.chunks[doc.id])


@pytest.mark.parametrize(
    ("objects", "reason"),
    [
        (lambda key: {key: b"sealed:" + image_pdf(400, 300)}, "sin OCR"),
        (lambda key: {key: b"sealed:%PDF-1.7 roto"}, "UnreadablePDF"),
        (lambda key: {}, "KeyError"),
    ],
)
async def test_every_failure_leaves_the_document_failed_with_its_reason(memory: Memory, objects, reason: str) -> None:
    doc = claimed()
    memory.pending.append(doc)
    ingestor, _ = build(memory, objects(doc.object_key))

    assert await ingestor.next() is True
    status, error = memory.status[doc.id]
    assert status == "failed" and reason in (error or "")
    assert doc.id not in memory.chunks


async def test_a_scanned_pdf_is_failed_before_its_images_cost_anything(memory: Memory) -> None:
    doc = claimed()
    memory.pending.append(doc)

    class Counting:
        provider = "counting"
        calls = 0

        async def complete(self, **_: object):
            Counting.calls += 1
            raise AssertionError("an image of a PDF without text was sent to describe")

    ingestor, _ = build(memory, {doc.object_key: b"sealed:" + image_pdf(400, 300)}, describer_client=Counting())
    await ingestor.next()
    assert memory.status[doc.id][0] == "failed" and Counting.calls == 0


async def test_the_sweep_releases_the_stale_and_unindexes_the_removed(memory: Memory) -> None:
    removed = repository.Removed(id=uuid4(), collection_name="kb_agente")
    memory.removed.append(removed)
    memory.stale.append(uuid4())
    ingestor, vectors = build(memory, {})
    vectors.points["kb_agente"] = [Point(id=uuid4(), vector=[1.0], document_id=removed.id)]

    await ingestor.sweep(lock_timeout_seconds=900)

    # Qdrant first and the chunks after, so a failure in between never marks gone what can still be found (ADR-0064, regla 9).
    assert memory.log == [f"delete {removed.id}", f"retire {removed.id}"]
    assert vectors.points["kb_agente"] == []
    assert memory.stale == []

