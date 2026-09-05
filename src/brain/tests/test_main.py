# main owns the exit code: a configuration that cannot serve has to stop the process, not the first turn.

import pytest

from infra.config import Settings, get_settings
from main import main, run_worker


def test_a_broken_configuration_returns_a_failing_exit_code(clean_env, monkeypatch, capsys) -> None:
    monkeypatch.setenv("DATABASE_URL", "no-es-un-dsn")
    monkeypatch.setenv("BRAIN_INTERNAL_TOKEN", "test-token")
    get_settings.cache_clear()

    assert main(["api"]) == 1
    assert "DATABASE_URL" in capsys.readouterr().err

    get_settings.cache_clear()


# The worker mode is declared by the deployment contract, so it has to fail loudly instead of pretending to run.
def test_the_worker_mode_says_it_is_not_implemented(settings: Settings) -> None:
    with pytest.raises(SystemExit, match="F06"):
        run_worker(settings)


def test_an_unknown_mode_is_rejected_before_anything_starts() -> None:
    with pytest.raises(SystemExit):
        main(["cerebro"])
