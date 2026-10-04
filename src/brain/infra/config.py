# Brain configuration, read from the environment and validated at boot (regla 10).

from functools import lru_cache
from pathlib import Path
from typing import Literal

from pydantic import Field, PostgresDsn, ValidationError, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

# Anchored to this file and not to the working directory, and absent in a container, where there is no repository above the code.
def _env_file() -> Path | None:
    parents = Path(__file__).resolve().parents
    return parents[3] / ".env" if len(parents) > 3 else None


# Raised when the environment cannot serve, so the failure is a stopped process and not a broken turn.
class ConfigError(RuntimeError):
    pass


# An allowlist and not a ban on production: with a ban, an ENVIRONMENT typed "prod" would quietly let canned answers through.
DOUBLE_ALLOWED_ENVIRONMENTS = frozenset({"development", "test"})


# Settings holds every value the brain needs: the shared database (ADR-0017) and the token the core signs with (ADR-0017 (k)).
class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=_env_file(),
        env_file_encoding="utf-8",
        extra="ignore",
    )

    environment: str = "development"
    log_level: Literal["debug", "info", "warning", "error"] = "info"

    brain_http_host: str = "0.0.0.0"
    brain_http_port: int = Field(default=8000, ge=1, le=65535)

    database_url: PostgresDsn
    db_pool_min_size: int = Field(default=1, ge=1)
    db_pool_max_size: int = Field(default=10, ge=1)

    brain_internal_token: str = Field(min_length=1)

    # No key can be required at boot: the provider is per agent (ai_agents.llm_provider), so a missing one fails when it runs.
    cohere_api_key: str = ""
    anthropic_api_key: str = ""
    openai_api_key: str = ""

    # Under the 15 s the core waits for the whole turn (BRAIN_TIMEOUT_MS), with room left for the reads around it.
    llm_timeout_seconds: float = Field(default=8.0, gt=0)

    # Development and CI answer with the doubled adapter because there is no key; production must never be able to.
    llm_use_double: bool = False

    # The worker of F06 (ADR-0064, ADR-0067, ADR-0069). Not required at boot here, because the api mode has no use for them and
    # must not even receive the document key (ADR-0064, regla 2): worker_problems() demands them when the worker starts.
    document_encryption_key: str = ""
    firebase_storage_bucket: str = ""
    # The variable Google's own client reads; it is named here only to keep the emulator out of production, as the core does.
    storage_emulator_host: str = ""
    storage_timeout_seconds: float = Field(default=120.0, gt=0)
    voyage_api_key: str = ""
    voyage_timeout_seconds: float = Field(default=30.0, gt=0)
    # The doubled embeddings, for the smoke and development: no key, and no document leaves for Voyage (ADR-0071).
    embeddings_use_double: bool = False
    qdrant_url: str = ""
    qdrant_api_key: str = ""
    qdrant_timeout_seconds: float = Field(default=30.0, gt=0)
    # Longer than a turn's 8 s: nobody is waiting on an ingestion, and a vision model takes its time with a dense page.
    description_timeout_seconds: float = Field(default=60.0, gt=0)
    # How often the worker looks for pending documents, like the turn's poller looks for closed batches (ADR-0041).
    worker_poll_interval_seconds: float = Field(default=5.0, gt=0)
    # ADR-0064, regla 8: longer than the slowest ingestion of a 100 MB document, so a live worker is never taken for a dead one.
    worker_lock_timeout_seconds: float = Field(default=900.0, gt=0)
    # What the turn waits for Voyage and for Qdrant when it searches (ADR-0074): each call, inside BRAIN_TIMEOUT_MS. Shorter than
    # the worker's two, because a customer is waiting on this one and nobody waits on an ingestion.
    retrieval_timeout_seconds: float = Field(default=5.0, gt=0)

    # Whether the api can search the knowledge base: Qdrant named, and a way to embed the question. Without it the api still
    # answers, as an agent without a base does (ADR-0068, regla 10), and says so at boot (ADR-0074).
    def knowledge_ready(self) -> bool:
        return bool(self.qdrant_url) and (bool(self.voyage_api_key) or self.embeddings_use_double)

    @model_validator(mode="after")
    def _double_stays_out_of_production(self) -> "Settings":
        if self.llm_use_double and self.environment not in DOUBLE_ALLOWED_ENVIRONMENTS:
            raise ValueError(
                "LLM_USE_DOUBLE answers canned text and is only allowed when ENVIRONMENT is "
                + " or ".join(sorted(DOUBLE_ALLOWED_ENVIRONMENTS))
            )
        return self

    @model_validator(mode="after")
    def _doubles_of_the_worker_stay_out_of_production(self) -> "Settings":
        if self.embeddings_use_double and self.environment not in DOUBLE_ALLOWED_ENVIRONMENTS:
            raise ValueError(
                "EMBEDDINGS_USE_DOUBLE embeds nothing real and is only allowed when ENVIRONMENT is "
                + " or ".join(sorted(DOUBLE_ALLOWED_ENVIRONMENTS))
            )
        if self.storage_emulator_host and self.environment not in DOUBLE_ALLOWED_ENVIRONMENTS:
            raise ValueError(
                "STORAGE_EMULATOR_HOST reads the documents from a fake store and is only allowed when ENVIRONMENT is "
                + " or ".join(sorted(DOUBLE_ALLOWED_ENVIRONMENTS))
            )
        return self

    # What the worker cannot start without, listed all at once like the boot does (regla 10). Asked by the worker and not by the
    # api, so a brain that only answers turns needs none of them.
    def worker_problems(self) -> list[str]:
        problems: list[str] = []
        if not self.document_encryption_key:
            problems.append("DOCUMENT_ENCRYPTION_KEY is required by the worker")
        else:
            from worker.crypto import decode_key

            try:
                decode_key(self.document_encryption_key)
            except ValueError as err:
                problems.append(str(err))
        if not self.firebase_storage_bucket:
            problems.append("FIREBASE_STORAGE_BUCKET is required by the worker")
        if not self.qdrant_url:
            problems.append("QDRANT_URL is required by the worker")
        if not self.voyage_api_key and not self.embeddings_use_double:
            problems.append("VOYAGE_API_KEY is required by the worker unless EMBEDDINGS_USE_DOUBLE is set")
        return problems

    @model_validator(mode="after")
    def _pool_range(self) -> "Settings":
        if self.db_pool_max_size < self.db_pool_min_size:
            raise ValueError("DB_POOL_MAX_SIZE cannot be smaller than DB_POOL_MIN_SIZE")
        return self


# Reads and validates the configuration once per process, so every caller shares one instance.
@lru_cache(maxsize=1)
def get_settings() -> Settings:
    try:
        return Settings()
    except ValidationError as err:
        raise ConfigError(describe(err)) from err


# Turns Pydantic's dump into one line per problem, the same shape the core prints (regla 10).
def describe(err: ValidationError) -> str:
    problems = [
        f"  {_variable(e['loc'])}: {e['msg'].removeprefix('Value error, ')}"
        for e in err.errors()
    ]
    return "\n".join(["config: the environment cannot serve the brain", *problems])


# Names the offending environment variable, falling back to the whole environment for a cross-field rule.
def _variable(loc: tuple[object, ...]) -> str:
    return ".".join(str(part) for part in loc).upper() or "environment"
