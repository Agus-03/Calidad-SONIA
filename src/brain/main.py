# Brain entrypoint (deployable 2 of ADR-0017): one process with an api|worker flag.

import argparse
import asyncio
import logging
import sys
from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager

from fastapi import APIRouter, Depends, FastAPI

from adapters.embeddings import registry as embeddings
from adapters.vectorstore.qdrant import QdrantStore
from api.routes import health, models, prompts
from api.routes.turns import route as turns
from api.security import require_internal_token
from infra import logging as sonia_logging
from infra.config import ConfigError, Settings, get_settings
from infra.db import Database
from knowledge import search
from knowledge.search import Searcher

logger = logging.getLogger("sonia.main")


# Opens the pool with the process and closes it on shutdown, the equivalent of the core's deferred pool.Close().
@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None]:
    settings: Settings = app.state.settings
    database = Database(
        dsn=str(settings.database_url),
        min_size=settings.db_pool_min_size,
        max_size=settings.db_pool_max_size,
    )
    await database.open()
    app.state.database = database
    vectors = _knowledge(app, settings, database)
    try:
        yield
    finally:
        if vectors is not None:
            await vectors.close()
        await database.close()


# The search of F06 for the turn (ADR-0068, ADR-0074), with the turn's timeout on both calls. Only development and test reach
# the warning: anywhere else Settings refuses to start without the search (ADR-0077), so there the brain always searches.
def _knowledge(app: FastAPI, settings: Settings, database: Database) -> QdrantStore | None:
    if not settings.knowledge_ready():
        logger.warning("knowledge search disabled: QDRANT_URL, or VOYAGE_API_KEY or EMBEDDINGS_USE_DOUBLE, is missing")
        return None
    vectors = QdrantStore(settings.qdrant_url, settings.qdrant_api_key, settings.retrieval_timeout_seconds)
    app.state.searcher = Searcher(
        embedder=embeddings.resolve(settings, timeout=settings.retrieval_timeout_seconds),
        vectors=vectors,
        connection=database.connection,
        rules=search.rules(),
    )
    return vectors


# Builds the internal API, where /health stays outside authentication so the container healthcheck needs no secret.
def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or get_settings()

    app = FastAPI(
        title="SONIA — cerebro",
        description="API interna del cerebro: la consume el núcleo, desplegable 1 de ADR-0017.",
        # The version of the contract and not of the deployment: it moves when brain.openapi.json changes shape, never alone.
        version="0.2.0",
        lifespan=lifespan,
    )
    app.state.settings = settings
    # None until the lifespan builds it, and for good when the brain cannot search: the turn then retrieves nothing.
    app.state.searcher = None

    app.include_router(health.router)

    internal = APIRouter(dependencies=[Depends(require_internal_token)])
    internal.include_router(turns.router)
    internal.include_router(models.router)
    internal.include_router(prompts.router)
    app.include_router(internal)
    return app


# Starts the HTTP server with uvicorn's own logging disabled, so every line keeps the JSON shape of infra.logging.
def run_api(settings: Settings) -> None:
    import uvicorn

    if sys.platform == "win32":
        asyncio.set_event_loop_policy(asyncio.WindowsSelectorEventLoopPolicy())

    uvicorn.run(
        create_app(settings),
        host=settings.brain_http_host,
        port=settings.brain_http_port,
        log_config=None,
    )


# Batch mode: the document ingestion of F06 (ADR-0064). Its own requirements are checked here and not in Settings, so the api
# never needs the document key; a worker that cannot ingest stops at boot, listing why, like the api does (regla 10).
def run_worker(settings: Settings) -> int:
    if problems := settings.worker_problems():
        print("\n".join(["config: the environment cannot serve the worker", *(f"  {p}" for p in problems)]), file=sys.stderr)
        return 1

    from worker import loop

    if sys.platform == "win32":
        asyncio.set_event_loop_policy(asyncio.WindowsSelectorEventLoopPolicy())
    asyncio.run(loop.run(settings))
    return 0


# Owns the exit code, so a configuration that cannot serve stops the process instead of failing on the first turn.
def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="sonia-brain")
    parser.add_argument("mode", choices=("api", "worker"), help="modo de arranque")
    args = parser.parse_args(argv)

    try:
        settings = get_settings()
    except ConfigError as exc:
        print(exc, file=sys.stderr)
        return 1

    sonia_logging.configure(settings.log_level, settings.environment)
    logger.info("brain starting", extra={"context": {"mode": args.mode}})

    if args.mode == "api":
        run_api(settings)
        return 0
    return run_worker(settings)


if __name__ == "__main__":
    raise SystemExit(main())
