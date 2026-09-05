# The log is read when something already broke, so its shape is part of the contract with whoever is debugging.

import json
import logging

import pytest

from infra.logging import configure


# configure replaces the root handlers, so a test that does not restore them silences the ones that follow.
@pytest.fixture(autouse=True)
def restore_root_logger():
    root = logging.getLogger()
    handlers, level = list(root.handlers), root.level
    yield
    root.handlers, root.level = handlers, level


def _written_line(capsys: pytest.CaptureFixture[str]) -> dict:
    return json.loads(capsys.readouterr().out.strip())


# slog names this level WARN, and a query matching "warning" would silently miss half the system.
def test_the_warning_level_is_written_the_way_slog_writes_it(capsys) -> None:
    configure("warning", "test")
    logging.getLogger("sonia.demo").warning("algo")

    assert _written_line(capsys)["level"] == "WARN"


def test_every_line_carries_the_service_and_the_environment(capsys) -> None:
    configure("info", "test")
    logging.getLogger("sonia.demo").info("algo")

    line = _written_line(capsys)
    assert line["service"] == "brain"
    assert line["environment"] == "test"
    assert line["msg"] == "algo"
    assert "time" in line


# The trap: a field outside context does not fail, does not warn, and does not appear.
def test_only_the_context_dict_reaches_the_json(capsys) -> None:
    configure("info", "test")
    logging.getLogger("sonia.demo").info("algo", extra={"context": {"agent_id": "a"}, "suelto": "b"})

    line = _written_line(capsys)
    assert line["agent_id"] == "a"
    assert "suelto" not in line


# The traceback is the whole reason the line exists, so it travels inside the JSON and not next to it.
def test_an_exception_reaches_the_json_line(capsys) -> None:
    configure("error", "test")
    try:
        raise ValueError("boom")
    except ValueError:
        logging.getLogger("sonia.demo").error("fallo el paso", exc_info=True)

    assert "ValueError: boom" in _written_line(capsys)["exception"]


def test_a_record_below_the_configured_level_is_not_written(capsys) -> None:
    configure("warning", "test")
    logging.getLogger("sonia.demo").info("no sale")

    assert capsys.readouterr().out == ""
