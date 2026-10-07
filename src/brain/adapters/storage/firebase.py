# The bytes the core uploaded to Cloud Storage for Firebase (ADR-0069). The worker only reads them: writing is the core's (ADR-0019).

import asyncio
from typing import Any

from google.auth.credentials import AnonymousCredentials
from google.cloud import storage


# The one failure the worker needs to tell apart: the object is not there, or the store did not answer.
class StorageError(Exception):
    pass


class FirebaseStorage:
    # The emulator takes no credential and names no project, which is how Google's own client reads STORAGE_EMULATOR_HOST; the
    # real service reads GOOGLE_APPLICATION_CREDENTIALS, the same two variables the core reads (ADR-0070).
    def __init__(self, bucket: str, timeout: float, *, emulated: bool, client: Any | None = None) -> None:
        if client is None:
            client = storage.Client(project="sonia", credentials=AnonymousCredentials()) if emulated else storage.Client()
        self._bucket = client.bucket(bucket)
        self._timeout = timeout

    async def download(self, key: str) -> bytes:
        try:
            # The client is synchronous, so it runs in a thread and the worker's loop keeps sweeping meanwhile.
            return await asyncio.to_thread(self._bucket.blob(key).download_as_bytes, timeout=self._timeout)
        except Exception as err:  # noqa: BLE001 - the boundary with a third-party SDK, whose exceptions are its own business
            raise StorageError(f"storage: cannot read {key}: {type(err).__name__}: {err}") from err
