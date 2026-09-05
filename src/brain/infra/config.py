# Brain configuration, read from the environment and validated at boot (regla 10).

from functools import lru_cache
from pathlib import Path
from typing import Literal

from pydantic import Field, PostgresDsn, ValidationError, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

# Anchored to this file and not to the working directory: there is one .env and it lives at the repository root.
_REPO_ROOT = Path(__file__).resolve().parents[3]


# Raised when the environment cannot serve, so the failure is a stopped process and not a broken turn.
class ConfigError(RuntimeError):
    pass


# Settings holds every value the brain needs: the shared database (ADR-0017) and the token the core signs with (ADR-0017 (k)).
class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=_REPO_ROOT / ".env",
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
