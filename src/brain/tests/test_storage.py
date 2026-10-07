# ADR-0069: the worker reads the object the core uploaded, and a store that fails is its own error.

import pytest

from adapters.storage import firebase
from adapters.storage.firebase import FirebaseStorage, StorageError


class FakeBlob:
    def __init__(self, objects: dict[str, bytes], key: str) -> None:
        self.objects, self.key = objects, key

    def download_as_bytes(self, *, timeout: float) -> bytes:
        assert timeout == 120
        return self.objects[self.key]


class FakeBucket:
    def __init__(self, objects: dict[str, bytes]) -> None:
        self.objects = objects

    def blob(self, key: str) -> FakeBlob:
        return FakeBlob(self.objects, key)


class FakeClient:
    def __init__(self, objects: dict[str, bytes]) -> None:
        self.objects = objects
        self.bucket_name = ""

    def bucket(self, name: str) -> FakeBucket:
        self.bucket_name = name
        return FakeBucket(self.objects)


async def test_the_object_is_read_whole_from_the_bucket_of_the_settings() -> None:
    client = FakeClient({"knowledge-bases/kb/doc": b"sealed"})
    storage = FirebaseStorage("sonia-prod", 120, emulated=False, client=client)

    assert await storage.download("knowledge-bases/kb/doc") == b"sealed"
    assert client.bucket_name == "sonia-prod"
    with pytest.raises(StorageError, match="knowledge-bases/kb/missing"):
        await storage.download("knowledge-bases/kb/missing")


# The emulator takes no credential, which is how the smoke reads what the core wrote to fake-gcs-server (ADR-0070).
def test_the_emulator_is_reached_without_a_credential(monkeypatch: pytest.MonkeyPatch) -> None:
    built: list[dict[str, object]] = []
    monkeypatch.setattr(firebase.storage, "Client", lambda **kwargs: built.append(kwargs) or FakeClient({}))

    FirebaseStorage("sonia-smoke", 120, emulated=True)
    FirebaseStorage("sonia-prod", 120, emulated=False)

    assert isinstance(built[0]["credentials"], firebase.AnonymousCredentials)
    assert built[1] == {}
