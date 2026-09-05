# Process logger: one JSON object per line to stdout, in the shape slog writes so one query covers both services.

import json
import logging
import sys
from datetime import UTC, datetime
from typing import Any

_LEVELS = {
    "debug": logging.DEBUG,
    "info": logging.INFO,
    "warning": logging.WARNING,
    "error": logging.ERROR,
}

# slog calls this level WARN while Python calls it WARNING, and the label is what a log query matches on.
_SLOG_LEVELS = {"WARNING": "WARN"}


class _JSONFormatter(logging.Formatter):
    def __init__(self, environment: str) -> None:
        super().__init__()
        self._environment = environment

    # Renders the record with slog's keys, and merges context at the top level because slog puts its attributes there.
    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "time": datetime.fromtimestamp(record.created, UTC).isoformat(),
            "level": _SLOG_LEVELS.get(record.levelname, record.levelname),
            "msg": record.getMessage(),
            "service": "brain",
            "environment": self._environment,
            "logger": record.name,
        }
        if record.exc_info:
            payload["exception"] = self.formatException(record.exc_info)

        context = getattr(record, "context", None)
        if isinstance(context, dict):
            payload.update(context)
        return json.dumps(payload, ensure_ascii=False)


# Installs the handler on the root logger, so uvicorn and psycopg log in the same shape instead of their own.
def configure(level: str, environment: str) -> None:
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(_JSONFormatter(environment))

    root = logging.getLogger()
    root.handlers = [handler]
    root.setLevel(_LEVELS.get(level, logging.INFO))
