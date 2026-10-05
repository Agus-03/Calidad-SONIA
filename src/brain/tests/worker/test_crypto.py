# ADR-0064, regla 1: the worker opens what the core sealed, in the format of ADR-0031.

import base64
import json
import os
from pathlib import Path

import pytest
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

from worker.crypto import CorruptedDocument, DocumentCipher, decode_key

# Sealed by the core's own encryption package: its Go test opens the same file, so a format change on either side fails here.
_VECTOR = Path(__file__).resolve().parents[4] / "contracts" / "fixtures" / "document-cipher.json"


def test_the_shared_vector_the_core_sealed_opens_here() -> None:
    vector = json.loads(_VECTOR.read_text(encoding="utf-8"))
    cipher = DocumentCipher(decode_key(vector["key"]))
    assert cipher.open(base64.b64decode(vector["sealed"])) == base64.b64decode(vector["plaintext"])


def _seal(key: bytes, plaintext: bytes, version: int = 1) -> bytes:
    nonce = os.urandom(12)
    head = bytes([version])
    return head + nonce + AESGCM(key).encrypt(nonce, plaintext, head)


def test_what_does_not_open_with_the_key_is_refused_and_not_returned() -> None:
    key = os.urandom(32)
    cipher = DocumentCipher(key)
    sealed = _seal(key, b"%PDF-1.7 contrato")
    assert cipher.open(sealed) == b"%PDF-1.7 contrato"

    tampered = bytearray(sealed)
    tampered[-1] ^= 1
    for bad in (bytes(tampered), sealed[:10], _seal(key, b"x", version=2), _seal(os.urandom(32), b"x")):
        with pytest.raises(CorruptedDocument):
            cipher.open(bad)


@pytest.mark.parametrize("encoded", ["esto no es base64!!", base64.b64encode(bytes(16)).decode(), ""])
def test_a_key_the_core_would_refuse_is_refused_here_too(encoded: str) -> None:
    with pytest.raises(ValueError):
        decode_key(encoded)
