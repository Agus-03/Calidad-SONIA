# Brain entrypoint (deployable 2 of ADR-0017): one process with an api|worker flag.

import argparse
import asyncio
import logging
import sys
from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager

from fastapi import APIRouter, Depends, FastAPI

from api.routes import health, turns
from api.security import require_internal_token
from infra import logging as sonia_logging
from infra.config import ConfigError, Settings, get_settings
from infra.db import Database

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
    try:
        yield
    finally:
        await database.close()


# Builds the internal API, where /health stays outside authentication so the container healthcheck needs no secret.
def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or get_settings()

    app = FastAPI(
        title="SONIA — cerebro",
        description="API interna del cerebro: la consume el núcleo, desplegable 1 de ADR-0017.",
        # The version of the contract and not of the deployment: it moves when contracts/brain.openapi.json
        # changes shape, and never on its own -- a bump nobody meant turns the whole generated file into noise.
        version="0.1.0",
        lifespan=lifespan,
    )
    app.state.settings = settings

    app.include_router(health.router)

    internal = APIRouter(dependencies=[Depends(require_internal_token)])
    internal.include_router(turns.router)
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


# Batch mode: the document ingestion of F06, declared by the deployment contract and not implemented yet.
def run_worker(settings: Settings) -> None:
    raise SystemExit("worker mode is not implemented yet: it is the document ingestion of F06")


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
    else:
        run_worker(settings)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
