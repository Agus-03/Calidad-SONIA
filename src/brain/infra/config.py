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

    # Under the 10 s the core waits for the whole turn (BRAIN_TIMEOUT_MS), with room left for the reads around it.
    llm_timeout_seconds: float = Field(default=8.0, gt=0)

    # Development and CI answer with the doubled adapter because there is no key; production must never be able to.
    llm_use_double: bool = False

    @model_validator(mode="after")
    def _double_stays_out_of_production(self) -> "Settings":
        if self.llm_use_double and self.environment not in DOUBLE_ALLOWED_ENVIRONMENTS:
            raise ValueError(
                "LLM_USE_DOUBLE answers canned text and is only allowed when ENVIRONMENT is "
                + " or ".join(sorted(DOUBLE_ALLOWED_ENVIRONMENTS))
            )
        return self

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
