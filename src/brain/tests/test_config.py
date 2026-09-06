# The configuration is the only thing between a missing secret and a process that answers turns wrong.

import pytest
from pydantic import ValidationError

from infra.config import DOUBLE_ALLOWED_ENVIRONMENTS, ConfigError, Settings, describe, get_settings

_MINIMUM = {
    "database_url": "postgresql://sonia:sonia@127.0.0.1:5432/sonia",
    "brain_internal_token": "test-token",
}


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


# Regla 10 stops a process that cannot serve, and a provider key is not that: it is per agent, and which agents exist
# is only known by reading the database. A missing one has to surface when that agent runs, not at boot.
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
