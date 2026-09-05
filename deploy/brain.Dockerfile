# Deployable 2 of ADR-0017: the locked dependencies first, the code after, so a code change does not reinstall them.

FROM python:3.14-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    POETRY_VERSION=2.4.3 \
    POETRY_VIRTUALENVS_CREATE=false

RUN pip install --no-cache-dir "poetry==${POETRY_VERSION}"

WORKDIR /app

# The lock file and not pyproject alone, so the image gets the same versions the tests ran against.
COPY src/brain/pyproject.toml src/brain/poetry.lock ./
RUN poetry install --only main --no-interaction

COPY src/brain ./

RUN useradd --uid 10001 --create-home sonia
USER sonia
EXPOSE 8000

CMD ["python", "main.py", "api"]
