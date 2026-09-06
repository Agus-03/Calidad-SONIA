# Shared fixtures: no test may read the .env of the machine, or it would pass or fail depending on who runs it.

import asyncio
import sys

import pytest

from infra.config import Settings

_BRAIN_VARIABLES = (
    "DATABASE_URL",
    "BRAIN_INTERNAL_TOKEN",
    "ENVIRONMENT",
    "LOG_LEVEL",
    "BRAIN_HTTP_HOST",
    "BRAIN_HTTP_PORT",
    "DB_POOL_MIN_SIZE",
    "DB_POOL_MAX_SIZE",
    "COHERE_API_KEY",
    "ANTHROPIC_API_KEY",
    "OPENAI_API_KEY",
    "LLM_TIMEOUT_SECONDS",
    "LLM_USE_DOUBLE",
)


# psycopg async cannot run on the Windows default event loop, and the deployment these tests mirror is Linux.
@pytest.fixture(scope="session")
def event_loop_policy():
    if sys.platform == "win32":
        return asyncio.WindowsSelectorEventLoopPolicy()
    return asyncio.get_event_loop_policy()


# Empties every variable the brain reads, so a test starts from a known environment and not from the shell.
@pytest.fixture
def clean_env(monkeypatch: pytest.MonkeyPatch) -> None:
    for name in _BRAIN_VARIABLES:
        monkeypatch.delenv(name, raising=False)


# A configuration that can serve, built in memory so nothing touches PostgreSQL.
@pytest.fixture
def settings() -> Settings:
    return Settings(
        _env_file=None,
        database_url="postgresql://sonia:sonia@127.0.0.1:5432/sonia",
        brain_internal_token="test-token",
    )
