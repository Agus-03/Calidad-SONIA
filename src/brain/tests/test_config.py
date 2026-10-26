# The configuration is the only thing between a missing secret and a process that answers turns wrong.

import pytest
from pathlib import Path
from dotenv import dotenv_values
from pydantic import ValidationError

from infra.config import DOUBLE_ALLOWED_ENVIRONMENTS, ConfigError, Settings, describe, get_settings

_MINIMUM = {
    "database_url": "postgresql://sonia:sonia@127.0.0.1:5432/sonia",
    "brain_internal_token": "test-token",
}


def test_example_environment_can_boot_brain_and_worker(clean_env, monkeypatch) -> None:
    # QA-001: empty numeric values in the distributed example overrode valid defaults.
    example = Path(__file__).resolve().parents[3] / ".env.example"
    for name, value in dotenv_values(example).items():
        monkeypatch.setenv(name, value or "")
    monkeypatch.setenv("DATABASE_URL", _MINIMUM["database_url"])
    monkeypatch.setenv("BRAIN_INTERNAL_TOKEN", _MINIMUM["brain_internal_token"])
    monkeypatch.setenv("DOCUMENT_ENCRYPTION_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
    monkeypatch.setenv("FIREBASE_STORAGE_BUCKET", "sonia-test")
    monkeypatch.setenv("EMBEDDINGS_USE_DOUBLE", "true")

    settings = Settings(_env_file=None)

    assert settings.brain_http_port == 8000
    assert (settings.db_pool_min_size, settings.db_pool_max_size) == (1, 10)
    assert settings.worker_problems() == []


# Failing on the first problem would mean booting four times to find four broken variables.
def test_every_problem_is_reported_at_once(clean_env, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("LOG_LEVEL", "verbose")
    monkeypatch.setenv("BRAIN_HTTP_PORT", "70000")

    with pytest.raises(ValidationError) as caught:
        Settings(_env_file=None)

    message = describe(caught.value)
    for variable in ("DATABASE_URL", "BRAIN_INTERNAL_TOKEN", "LOG_LEVEL", "BRAIN_HTTP_PORT"):
        assert variable in message


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("log_level", "verbose"),
        ("brain_internal_token", ""),
        ("db_pool_min_size", 0),
        ("brain_http_port", 70000),
        ("database_url", "no-es-un-dsn"),
    ],
)
def test_an_environment_that_cannot_serve_does_not_start(clean_env, field: str, value: object) -> None:
    with pytest.raises(ValidationError):
        Settings(_env_file=None, **{**_MINIMUM, field: value})


# LOG_LEVEL is shared with the core, so one spelling has to win and warn is not it.
def test_the_shared_log_level_is_spelled_warning(clean_env) -> None:
    assert Settings(_env_file=None, **_MINIMUM, log_level="warning").log_level == "warning"

    with pytest.raises(ValidationError):
        Settings(_env_file=None, **_MINIMUM, log_level="warn")


def test_the_pool_ceiling_cannot_sit_below_its_floor(clean_env) -> None:
    with pytest.raises(ValidationError):
        Settings(_env_file=None, **_MINIMUM, db_pool_min_size=5, db_pool_max_size=2)


def test_the_defaults_are_the_ones_the_deployment_assumes(clean_env) -> None:
    settings = Settings(_env_file=None, **_MINIMUM)

    assert (settings.brain_http_host, settings.brain_http_port) == ("0.0.0.0", 8000)
    assert settings.log_level == "info"
    assert (settings.db_pool_min_size, settings.db_pool_max_size) == (1, 10)


# get_settings is what main calls, and a raw ValidationError there is unreadable at the worst possible moment.
def test_get_settings_raises_a_readable_error(clean_env, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "no-es-un-dsn")
    monkeypatch.setenv("BRAIN_INTERNAL_TOKEN", "test-token")
    get_settings.cache_clear()

    with pytest.raises(ConfigError) as caught:
        get_settings()

    assert "DATABASE_URL" in str(caught.value)
    get_settings.cache_clear()


# Regla 10 stops a process that cannot serve, and a provider key is not that: it is per agent and has to fail when it runs.
def test_a_provider_key_is_not_required_to_boot(clean_env) -> None:
    settings = Settings(_env_file=None, **_MINIMUM)

    assert (settings.cohere_api_key, settings.anthropic_api_key, settings.openai_api_key) == ("", "", "")


# The double answers canned text, so letting it into production would be a bot that looks alive and says nothing real.
@pytest.mark.parametrize("environment", ["production", "staging", "prod", "produccion", ""])
def test_the_double_cannot_be_switched_on_outside_development(clean_env, environment: str) -> None:
    with pytest.raises(ValidationError):
        Settings(_env_file=None, **_MINIMUM, llm_use_double=True, environment=environment)


# An allowlist and not a ban: these two are the only spellings that may answer with canned text.
@pytest.mark.parametrize("environment", sorted(DOUBLE_ALLOWED_ENVIRONMENTS))
def test_the_double_is_allowed_where_there_is_no_key(clean_env, environment: str) -> None:
    assert Settings(_env_file=None, **_MINIMUM, llm_use_double=True, environment=environment).llm_use_double


# The core waits BRAIN_TIMEOUT_MS for the whole turn, so this one has to leave room for the reads around the call.
def test_the_provider_timeout_fits_inside_the_turn_budget(clean_env) -> None:
    assert Settings(_env_file=None, **_MINIMUM).llm_timeout_seconds < 10

    with pytest.raises(ValidationError):
        Settings(_env_file=None, **_MINIMUM, llm_timeout_seconds=0)


# The two stand-ins of the worker follow the rule of the LLM double: a fake store or fake vectors in production would be an
# ingestion that looks done and indexes nothing real (ADR-0071).
@pytest.mark.parametrize("field", [{"embeddings_use_double": True}, {"storage_emulator_host": "storage:4443"}])
def test_the_worker_doubles_stay_out_of_production(clean_env, field: dict[str, object]) -> None:
    with pytest.raises(ValidationError):
        Settings(_env_file=None, **_MINIMUM, environment="production", **field)
    assert Settings(_env_file=None, **_MINIMUM, environment="test", **field)


# ADR-0077: outside development and test, a brain that cannot search does not start, because every agent would answer without
# its documents. Inside them it still starts, and the lifespan warns.
@pytest.mark.parametrize("environment", ["production", "staging", "prod", ""])
def test_the_search_is_required_outside_development(clean_env, environment: str) -> None:
    for missing in ({}, {"qdrant_url": "http://qdrant:6333"}, {"voyage_api_key": "k"}):
        with pytest.raises(ValidationError, match="QDRANT_URL and VOYAGE_API_KEY"):
            Settings(_env_file=None, **_MINIMUM, environment=environment, **missing)
    ready = Settings(_env_file=None, **_MINIMUM, environment=environment, qdrant_url="http://qdrant:6333", voyage_api_key="k")
    assert ready.knowledge_ready()


@pytest.mark.parametrize("environment", sorted(DOUBLE_ALLOWED_ENVIRONMENTS))
def test_development_and_test_still_start_without_the_search(clean_env, environment: str) -> None:
    assert not Settings(_env_file=None, **_MINIMUM, environment=environment).knowledge_ready()


# The api boots without any of the worker's variables, and must: it does not even receive the document key (ADR-0064, regla 2).
def test_the_worker_lists_everything_it_is_missing_and_the_api_needs_none_of_it(clean_env) -> None:
    settings = Settings(_env_file=None, **_MINIMUM)
    problems = settings.worker_problems()
    assert len(problems) == 4

    ready = Settings(
        _env_file=None,
        **_MINIMUM,
        document_encryption_key="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
        firebase_storage_bucket="sonia",
        qdrant_url="http://qdrant:6333",
        voyage_api_key="k",
    )
    assert ready.worker_problems() == []

    short_key = ready.model_copy(update={"document_encryption_key": "c2hvcnQ="})
    assert any("32" in problem for problem in short_key.worker_problems())
    doubled = ready.model_copy(update={"voyage_api_key": "", "embeddings_use_double": True})
    assert doubled.worker_problems() == []
