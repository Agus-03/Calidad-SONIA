# Opens what the core sealed (ADR-0064, regla 1): AES-256-GCM in the format of ADR-0031, version (1 byte) ‖ nonce (12 bytes) ‖
# ciphertext ‖ tag (16 bytes), with the version as additional data. The core seals and this only opens: the worker never writes
# an object (ADR-0019).

import base64
import binascii

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

KEY_BYTES = 32
_VERSION = 0x01
_NONCE_BYTES = 12
_TAG_BYTES = 16


# A tampered, truncated or foreign object refuses instead of returning other bytes, which is what authenticated encryption buys.
class CorruptedDocument(Exception):
    pass


# The shape the environment carries the key in, refused at boot like the core refuses it (ADR-0064, regla 2).
def decode_key(encoded: str) -> bytes:
    try:
        key = base64.b64decode(encoded, validate=True)
    except (binascii.Error, ValueError) as err:
        raise ValueError("DOCUMENT_ENCRYPTION_KEY is not valid base64") from err
    if len(key) != KEY_BYTES:
        raise ValueError(f"DOCUMENT_ENCRYPTION_KEY decodes to {len(key)} bytes and needs {KEY_BYTES}")
    return key


class DocumentCipher:
    def __init__(self, key: bytes) -> None:
        self._aead = AESGCM(key)

    def open(self, sealed: bytes) -> bytes:
        if len(sealed) < 1 + _NONCE_BYTES + _TAG_BYTES:
            raise CorruptedDocument("the object is shorter than an empty sealed file")
        if sealed[0] != _VERSION:
            raise CorruptedDocument(f"unknown format version {sealed[0]:#x}")
        nonce = sealed[1 : 1 + _NONCE_BYTES]
        try:
            return self._aead.decrypt(nonce, sealed[1 + _NONCE_BYTES :], sealed[:1])
        except InvalidTag as err:
            raise CorruptedDocument("the object does not open with the document key") from err
