# The fixtures of the triage tests: a conftest and not an import, so every file of this folder gets a fresh rules() cache.

import pytest

from pipeline import triage


# rules() caches for the whole process, which is what production wants and what would otherwise let the order of these tests
# decide their result: the first one to run would fix the prompt for all the rest.
@pytest.fixture(autouse=True)
def _fresh_rules():
    triage.rules.cache_clear()
    yield
    triage.rules.cache_clear()
