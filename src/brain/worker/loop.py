# The worker mode of the brain (ADR-0017 (g)): the same code as the api, a second entrypoint. It polls, like the turn's timer
# does (ADR-0041): every interval it sweeps, and then drains every pending document before sleeping again.

import asyncio
import logging
import signal

from adapters.embeddings import registry as embeddings
from adapters.llm import registry as llm
from adapters.storage.firebase import FirebaseStorage
from adapters.vectorstore.qdrant import QdrantStore
from infra.config import Settings
from infra.db import Database
from knowledge import chunking
from worker import images
from worker.crypto import DocumentCipher, decode_key
from worker.ingest import Ingestor, Parts

logger = logging.getLogger("sonia.worker")


async def run(settings: Settings, *, stop: asyncio.Event | None = None) -> None:
    stop = stop or _stop_on_signals()
    # Read before anything opens, so a rules file that cannot be read stops the worker at boot and not on the first document.
    chunking_rules = chunking.rules()
    description_rules = images.rules()

    database = Database(dsn=str(settings.database_url), min_size=1, max_size=2)
    await database.open()
    vectors = QdrantStore(settings.qdrant_url, settings.qdrant_api_key, settings.qdrant_timeout_seconds)
    try:
        ingestor = Ingestor(
            Parts(
                objects=FirebaseStorage(
                    settings.firebase_storage_bucket,
                    settings.storage_timeout_seconds,
                    emulated=bool(settings.storage_emulator_host),
                ),
                opener=DocumentCipher(decode_key(settings.document_encryption_key)),
                embedder=embeddings.resolve(settings),
                vectors=vectors,
                describer=images.Describer(
                    llm.resolve(description_rules.provider, settings, timeout=settings.description_timeout_seconds),
                    description_rules,
                ),
                description_rules=description_rules,
                chunking_rules=chunking_rules,
                connection=database.connection,
            )
        )
        logger.info("worker ready", extra={"context": {"interval_seconds": settings.worker_poll_interval_seconds}})
        while not stop.is_set():
            await _tick(ingestor, settings)
            try:
                await asyncio.wait_for(stop.wait(), timeout=settings.worker_poll_interval_seconds)
            except TimeoutError:
                pass
    finally:
        await vectors.close()
        await database.close()
        logger.info("worker stopped")


# One failure of a tick is logged and the next tick tries again: a database or a Qdrant that blinked must not stop the worker.
async def _tick(ingestor: Ingestor, settings: Settings) -> None:
    try:
        await ingestor.sweep(lock_timeout_seconds=settings.worker_lock_timeout_seconds)
        while await ingestor.next():
            pass
    except Exception:  # noqa: BLE001 - the loop outlives one bad tick; the document's own failures are handled inside next()
        logger.exception("worker tick failed")


# SIGTERM is what docker stop sends: the worker finishes the document it holds and leaves, instead of dying in the middle of it.
def _stop_on_signals() -> asyncio.Event:
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        try:
            loop.add_signal_handler(sig, stop.set)
        except (NotImplementedError, RuntimeError):
            # Windows has no signal handlers on the event loop; Ctrl+C still raises KeyboardInterrupt there.
            pass
    return stop
