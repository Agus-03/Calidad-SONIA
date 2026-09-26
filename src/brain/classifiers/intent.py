# The classifier of F11: how many different things the customer asked for in one batch, and in what order (ADR-0041, regla 12).

import json
import logging
from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
from functools import lru_cache
from pathlib import Path

import yaml

from adapters.llm.base import LLMClient, LLMMessage, unfenced

logger = logging.getLogger("sonia.classifiers.intent")

# Anchored to the package and not to the working directory: the brain runs in a container, where neither is the repository.
_RULES_FILE = Path(__file__).resolve().parent.parent / "rules" / "intent.yaml"

# The scale of the example in 03-capa-prompts section 4.4, and enough for a number nobody compares beyond two digits.
_SCALE = Decimal("0.01")

# varchar with no limit in the schema, but a label is a label: anything longer is a sentence the model wrote instead of one.
_MAX_LABEL = 60


# Raised while loading and never while classifying: a prompt file that cannot be read is a deployment mistake.
class IntentRulesError(RuntimeError):
    pass


# Frozen because it is data on its way to a row, the same decision as the rest of the pipeline's dataclasses.
@dataclass(frozen=True)
class Intent:
    label: str
    confidence: Decimal
    # The order the customer said them in, which is what detected_intents.position stores, starting at 1.
    position: int


@dataclass(frozen=True)
class Rules:
    # Travels nowhere yet: detected_intents has no column for it, and that is declared as a gap, not as an oversight.
    version: int
    marker: str
    max_intents: int
    system: str
    user_heading: str


# Read once per process: the file is versioned data, so it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    return _load(_RULES_FILE)


def _load(path: Path) -> Rules:
    try:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as err:
        raise IntentRulesError(f"intent: {path.name} cannot be read: {err}") from err
    if not isinstance(raw, dict):
        raise IntentRulesError(f"intent: {path.name} is not a mapping")

    version = raw.get("version")
    if not isinstance(version, int) or isinstance(version, bool):
        raise IntentRulesError("intent: version has to be an integer, which is what names a prompt in a measurement")

    max_intents = raw.get("max_intents")
    if not isinstance(max_intents, int) or isinstance(max_intents, bool) or max_intents < 1:
        raise IntentRulesError("intent: max_intents has to be a positive integer")

    marker = _text(raw, "marker")
    system = _text(raw, "system")
    # Checked here because the doubled adapter finds the classifier by this literal: a prompt that stopped carrying it would
    # leave the doubled stack running every path of the turn except the one F11 adds, and nothing would say so.
    if marker not in system:
        raise IntentRulesError(f"intent: the system prompt does not open with the marker {marker!r}")

    return Rules(
        version=version,
        marker=marker,
        max_intents=max_intents,
        system=system,
        user_heading=_text(raw, "user"),
    )


def _text(parent: dict, key: str) -> str:
    value = parent.get(key)
    if not isinstance(value, str) or not value.strip():
        raise IntentRulesError(f"intent: intent.yaml has no {key} text")
    return value.strip()


# Takes no connection: the call takes seconds, and holding a pool connection through it starves the next turn.
# Never raises on a bad answer. The customer's reply does not depend on this, and losing the evidence of one turn is a smaller
# failure than not answering at all (ADR-0041, regla 12).
async def classify(*, client: LLMClient, model: str, message: str) -> tuple[Intent, ...]:
    spec = rules()

    completion = await client.complete(
        model=model,
        system=spec.system,
        messages=(LLMMessage(role="user", text=f"{spec.user_heading}\n{message.strip()}"),),
        # Zero so the same batch classifies the same way twice: this is evidence and not prose.
        temperature=0.0,
    )
    return parse(completion.text, spec.max_intents)


# Separate from the call so the parsing can be tested without a model, which is where every case below actually lives.
def parse(answer: str, max_intents: int) -> tuple[Intent, ...]:
    text = unfenced(answer)

    try:
        payload = json.loads(text)
    except ValueError:
        logger.warning("the intent classifier did not answer JSON", extra={"context": {"answer": text[:200]}})
        return ()

    if not isinstance(payload, list):
        logger.warning("the intent classifier answered JSON that is not a list", extra={"context": {"answer": text[:200]}})
        return ()

    intents: list[Intent] = []
    seen: set[str] = set()
    for item in payload:
        intent = _intent(item, len(intents) + 1)
        # The same label twice is one intention said two ways, which the prompt already asks the model not to do.
        if intent is None or intent.label in seen:
            continue
        seen.add(intent.label)
        intents.append(intent)
        if len(intents) == max_intents:
            break

    return tuple(intents)


# Returns None instead of raising: one malformed element must not throw away the ones that came out fine.
def _intent(item: object, position: int) -> Intent | None:
    if not isinstance(item, dict):
        return None

    label = item.get("label")
    if not isinstance(label, str) or not label.strip() or len(label.strip()) > _MAX_LABEL:
        return None

    try:
        # Through str(): a JSON float is already binary, and the row stores numeric.
        confidence = Decimal(str(item.get("confidence"))).quantize(_SCALE)
    except (InvalidOperation, ValueError, TypeError):
        return None
    if not Decimal(0) <= confidence <= Decimal(1):
        return None

    return Intent(label=label.strip(), confidence=confidence, position=position)
