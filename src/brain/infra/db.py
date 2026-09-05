# PostgreSQL access: the brain does DML, never DDL, and never writes conversations or messages (regla 3).

import logging
from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager

import psycopg
from psycopg import AsyncConnection
from psycopg.rows import dict_row
from psycopg_pool import AsyncConnectionPool

logger = logging.getLogger("sonia.db")


class Database:
    def __init__(self, dsn: str, min_size: int = 1, max_size: int = 10) -> None:
        self._pool = AsyncConnectionPool(
            conninfo=dsn,
            min_size=min_size,
            max_size=max_size,
            open=False,
            kwargs={"row_factory": dict_row},
        )

    # Opens and verifies the pool, because an unverified pool fails on the first turn with a customer waiting.
    async def open(self) -> None:
        await self._pool.open(wait=True, timeout=10.0)
        async with self.connection() as conn:
            await conn.execute("SELECT 1")
        logger.info("database pool ready")

    async def close(self) -> None:
        await self._pool.close()

    # Lends a connection, where the async with block is one psycopg transaction.
    @asynccontextmanager
    async def connection(self) -> AsyncGenerator[AsyncConnection]:
        async with self._pool.connection() as conn:
            yield conn

    # Reports whether the database still answers, so /health can tell a live process from a serving one.
    async def ping(self) -> bool:
        try:
            async with self.connection() as conn:
                await conn.execute("SELECT 1")
        except psycopg.Error:
            logger.warning("database ping failed", exc_info=True)
            return False
        return True
