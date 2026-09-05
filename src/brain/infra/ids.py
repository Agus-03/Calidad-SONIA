# Primary keys for the evidence rows the brain writes: UUID v7, generated in the application (C04).

from uuid import UUID, uuid7


# Wraps the stdlib so C04 lives in one place and nobody reaches for uuid4, which scatters the inserts and fragments the index.
def new() -> UUID:
    return uuid7()
