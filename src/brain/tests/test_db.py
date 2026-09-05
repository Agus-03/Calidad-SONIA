# The pool is what every evidence row goes through, and /health depends on it failing quietly.

from infra.db import Database


# ping has to answer and not raise: an exception here turns the health check into a 500 instead of a 503.
async def test_a_database_that_cannot_be_reached_reports_itself_as_down() -> None:
    database = Database("postgresql://sonia:sonia@127.0.0.1:1/sonia")

    assert await database.ping() is False
