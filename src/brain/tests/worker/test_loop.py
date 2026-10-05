# The worker's loop: it sweeps and drains on every tick, outlives a tick that fails, and closes what it opened when it stops.

import asyncio
import base64

import pytest

from infra.config import Settings
from worker import loop


@pytest.fixture
def ready(settings: Settings) -> Settings:
    return settings.model_copy(
        update={
            "document_encryption_key": base64.b64encode(bytes(32)).decode(),
            "firebase_storage_bucket": "sonia-test",
            "storage_emulator_host": "storage:4443",
            "qdrant_url": "http://qdrant:6333",
            "embeddings_use_double": True,
            "llm_use_double": True,
            "worker_poll_interval_seconds": 0.01,
        }
    )


class Closable:
    def __init__(self, *args: object, **kwargs: object) -> None:
        self.closed = False
        Closable.last = self

    async def open(self) -> None:
        pass

    async def close(self) -> None:
        self.closed = True

    def connection(self):  # pragma: no cover - the fake Ingestor never asks for one
        raise AssertionError


async def test_the_loop_ticks_until_it_is_stopped_and_closes_what_it_opened(ready: Settings, monkeypatch: pytest.MonkeyPatch) -> None:
    stop = asyncio.Event()
    ticks: list[str] = []

    class FakeIngestor:
        def __init__(self, parts: object) -> None:
            self.pending = 2

        async def sweep(self, *, lock_timeout_seconds: float) -> None:
            ticks.append("sweep")
            if ticks.count("sweep") == 2:
                raise RuntimeError("the database blinked")
            if ticks.count("sweep") == 3:
                stop.set()

        async def next(self) -> bool:
            ticks.append("next")
            self.pending -= 1
            return self.pending >= 0

    databases: list[Closable] = []
    stores: list[Closable] = []
    monkeypatch.setattr(loop, "Database", lambda **kwargs: databases.append(Closable()) or databases[-1])
    monkeypatch.setattr(loop, "QdrantStore", lambda *args: stores.append(Closable()) or stores[-1])
    monkeypatch.setattr(loop, "FirebaseStorage", lambda *args, **kwargs: object())
    monkeypatch.setattr(loop, "Ingestor", FakeIngestor)

    await asyncio.wait_for(loop.run(ready, stop=stop), timeout=5)

    # The first tick drains the two pending documents; the second fails and is logged; the third stops the loop.
    assert ticks[:4] == ["sweep", "next", "next", "next"]
    assert ticks.count("sweep") == 3
    assert databases[0].closed and stores[0].closed


# docker stop waits ten seconds: a worker asked to leave finishes the document it holds and does not take the next one.
async def test_a_stop_in_the_middle_of_a_queue_takes_no_further_document(ready: Settings) -> None:
    stop = asyncio.Event()
    taken: list[int] = []

    class Queue:
        async def sweep(self, *, lock_timeout_seconds: float) -> None:
            pass

        async def next(self) -> bool:
            taken.append(len(taken) + 1)
            stop.set()
            return True

    await loop._tick(Queue(), ready, stop)

    assert taken == [1]


async def test_the_signals_stop_the_worker() -> None:
    stop = loop._stop_on_signals()
    assert not stop.is_set()
