# The extraction of F31 (ADR-0047): the sensitive three by the patterns of the checkpoint, never by a model, and the other three by
# a classifier over the redacted text. The brain returns what it found; the core encrypts it and decides what is kept.

import json
import re
from collections.abc import Sequence
from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
from functools import lru_cache
from uuid import UUID

from adapters.llm.base import LLMClient, LLMMessage, LLMRefusal, unfenced
from infra.rules import RulesFileError, load
from pipeline import guardrail
from pipeline.repository.turn import Message

_FILE = "extraction.yaml"

# Enough of a bad answer to recognise it in a log; the text it came from was redacted before this ran (ADR-0006).
_MAX_REPORTED = 200

# The scale of the other confidences of the pipeline, and a pattern is certain of what it matched, which is why it writes 1.
_SCALE = Decimal("0.01")
_CERTAIN = Decimal("1.00")

# A name is a few words; anything longer is a sentence the model wrote instead of one.
_MAX_NAME = 60
_MAX_EMAIL = 254
_EMAIL = re.compile(r"^[^@\s]+@[^@\s]+\.[^@\s]+$")

# The three fields the parser reads, which extraction.yaml has to name exactly.
_BY_MODEL = ("first_name", "last_name", "email")


@dataclass(frozen=True)
class Rules:
    version: int
    marker: str
    sensitive: tuple[str, ...]
    by_model: tuple[str, ...]
    system: str
    user_heading: str


# What contact_attributes will hold once the core encrypted it: the key, the value in the clear and where it was said.
@dataclass(frozen=True)
class Extracted:
    key: str
    value: str
    confidence: Decimal
    source_message_id: UUID


# Read once per process: the file is versioned data, so it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    spec = load(_FILE)
    marker = spec.text("marker")
    system = spec.text("system")
    # Checked while loading, because the doubled adapter finds this call by that literal.
    if marker not in system:
        raise RulesFileError(f"extraction: the system prompt does not carry the marker {marker!r}")

    keys = spec.section("keys")
    sensitive, by_model = keys.strings("sensitive"), keys.strings("by_model")
    # The sensitive keys are taken by the patterns of guardrails.yaml, so each has to be one of its types, or it is never found.
    typed = {r.type for r in guardrail.rules().redactions}
    if missing := set(sensitive) - typed:
        raise RulesFileError(f"extraction: {sorted(missing)} are not types of guardrails.yaml, so nothing would ever find them")
    # The parser below knows three fields and no more: a fourth key in the file would be asked for and never read.
    if set(by_model) != set(_BY_MODEL):
        raise RulesFileError(f"extraction: by_model has to be {sorted(_BY_MODEL)}, the fields the parser reads")

    return Rules(
        version=spec.version,
        marker=marker,
        sensitive=sensitive,
        by_model=by_model,
        system=system,
        user_heading=spec.text("user"),
    )


# The checkpoint half: what the redaction replaces in each message of the batch, with that message as its origin. The last value of
# a key wins, because the customer who wrote two DNIs corrected the first one (ADR-0047, section 2.7).
def sensitive(incoming: Sequence[Message]) -> tuple[Extracted, ...]:
    kept = set(rules().sensitive)
    found: dict[str, Extracted] = {}
    for message in incoming:
        if message.id is None:
            continue
        for key, value in guardrail.found(message.content_text):
            if key in kept and value:
                found[key] = Extracted(key=key, value=value, confidence=_CERTAIN, source_message_id=message.id)
    return tuple(found.values())


# Takes no connection: the call takes seconds. It raises LLMError on an answer it cannot use, and the caller keeps the rest of the
# turn going, the same as with the intents (ADR-0041, regla 12).
async def classify(*, client: LLMClient, model: str, message: str, incoming: Sequence[Message]) -> tuple[Extracted, ...]:
    spec = rules()
    completion = await client.complete(
        model=model,
        system=spec.system,
        messages=(LLMMessage(role="user", text=f"{spec.user_heading}\n{message.strip()}"),),
        # Zero so the same batch extracts the same way twice: what it writes is kept as a datum of the contact.
        temperature=0.0,
    )
    values, confidence = parse(completion.text)
    return tuple(
        Extracted(key=key, value=value, confidence=confidence, source_message_id=origin)
        for key, value in values.items()
        if (origin := _origin(value, incoming)) is not None
    )



# Separate from the call so every case can be tested without a model. A field that is not usable is dropped, not fatal: the other
# two may still be right. An answer that is not the object at all is a refusal.
def parse(answer: str) -> tuple[dict[str, str], Decimal]:
    text = unfenced(answer)
    try:
        payload = json.loads(text)
    except ValueError as err:
        raise LLMRefusal(f"extraction: the model did not answer JSON: {text[:_MAX_REPORTED]}") from err
    if not isinstance(payload, dict):
        raise LLMRefusal(f"extraction: the model answered JSON that is not an object: {text[:_MAX_REPORTED]}")

    try:
        # Through str(): a JSON float is already binary, and the column is numeric.
        confidence = Decimal(str(payload.get("confidence"))).quantize(_SCALE)
    except (InvalidOperation, ValueError, TypeError):
        confidence = None
    if confidence is None or not Decimal(0) <= confidence <= Decimal(1):
        # Without it nothing is stored: a datum of the contact whose certainty nobody knows reads as certain in the column.
        raise LLMRefusal(f"extraction: {payload.get('confidence')!r} is not a confidence")

    values: dict[str, str] = {}
    for key in _BY_MODEL:
        value = payload.get(key)
        if not isinstance(value, str) or not value.strip():
            continue
        value = value.strip()
        if key == "email":
            if len(value) <= _MAX_EMAIL and _EMAIL.match(value):
                values[key] = value.lower()
        # A marker is what the redaction left, never a name: writing it would store "[DNI]" as the contact's surname.
        elif len(value) <= _MAX_NAME and "[" not in value:
            values[key] = value
    return values, confidence


# The message of the batch that holds the value; the last one when the model wrote it differently from the customer.
def _origin(value: str, incoming: Sequence[Message]) -> UUID | None:
    folded = value.casefold()
    with_id = [m for m in incoming if m.id is not None]
    for message in with_id:
        if folded in message.content_text.casefold():
            return message.id
    return with_id[-1].id if with_id else None
