# main owns the exit code: a configuration that cannot serve has to stop the process, not the first turn.

import base64

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


# The worker stops at boot when it cannot ingest, listing everything that is missing at once, like the api does (regla 10).
def test_a_worker_that_cannot_ingest_stops_at_boot(settings: Settings, capsys) -> None:
    assert run_worker(settings) == 1
    err = capsys.readouterr().err
    for variable in ("DOCUMENT_ENCRYPTION_KEY", "FIREBASE_STORAGE_BUCKET", "QDRANT_URL", "VOYAGE_API_KEY"):
        assert variable in err


# With what it needs, the worker hands the process to its loop and returns its exit code when the loop ends.
def test_a_configured_worker_runs_its_loop(settings: Settings, monkeypatch: pytest.MonkeyPatch) -> None:
    from worker import loop

    ran: list[Settings] = []

    async def fake_run(given: Settings) -> None:
        ran.append(given)

    monkeypatch.setattr(loop, "run", fake_run)
    ready = settings.model_copy(
        update={
            "document_encryption_key": base64.b64encode(bytes(32)).decode(),
            "firebase_storage_bucket": "sonia-test",
            "qdrant_url": "http://qdrant:6333",
            "embeddings_use_double": True,
        }
    )
    assert run_worker(ready) == 0
    assert ran == [ready]


def test_an_unknown_mode_is_rejected_before_anything_starts() -> None:
    with pytest.raises(SystemExit):
        main(["cerebro"])
