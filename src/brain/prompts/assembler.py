# The assembler of F03: the structure comes from rules/generation.yaml and the content from prompt_components.

import logging
from collections.abc import Sequence
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path

import yaml

from infra.enums import PromptComponentType
from prompts.repository import Component

logger = logging.getLogger("sonia.prompts.assembler")

# Anchored to the package and not to the working directory: the brain runs in a container, where neither is the repository.
_RULES_FILE = Path(__file__).resolve().parent.parent / "rules" / "generation.yaml"

# Replaced and not formatted, so a brace typed inside a component can never turn into a template error.
_AGENT_NAME = "{agent_name}"

_USER_SECTIONS = ("context", "knowledge", "message")


# Raised while loading and never while answering: a scaffolding that cannot be read is a deployment mistake.
class RulesError(RuntimeError):
    pass


@dataclass(frozen=True)
class Rules:
    version: int
    preamble: str
    headings: dict[PromptComponentType, str]
    closing: str
    user_headings: dict[str, str]


@dataclass(frozen=True)
class Prompt:
    system: str
    user: str
    # Travels with the prompt because regla 9 exists so a measurement can name the version that produced it.
    rules_version: int
    # How many of the six went in, which is the number OE1 counts.
    components: int


# Read once per process: the file is versioned data, so it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    return _load(_RULES_FILE)


def _load(path: Path) -> Rules:
    try:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as err:
        raise RulesError(f"prompts: {path.name} cannot be read: {err}") from err
    if not isinstance(raw, dict):
        raise RulesError(f"prompts: {path.name} is not a mapping")

    version = raw.get("version")
    if not isinstance(version, int):
        raise RulesError("prompts: generation.yaml has no integer version, and regla 9 exists for that field")

    system = _mapping(raw, "system")
    headings = _mapping(system, "headings")
    # Checked here and not at assembly time: a type with no heading would drop out of the prompt in silence, which OE1 measures.
    missing = [t for t in PromptComponentType if t.value not in headings]
    if missing:
        raise RulesError(f"prompts: no heading for {', '.join(t.value for t in missing)}")

    user_headings = _mapping(_mapping(raw, "user"), "headings")
    absent = [name for name in _USER_SECTIONS if name not in user_headings]
    if absent:
        raise RulesError(f"prompts: no heading for the user sections {', '.join(absent)}")

    return Rules(
        version=version,
        preamble=_text(system, "preamble"),
        headings={t: str(headings[t.value]) for t in PromptComponentType},
        closing=_text(system, "closing"),
        user_headings={name: str(user_headings[name]) for name in _USER_SECTIONS},
    )


def _mapping(parent: dict, key: str) -> dict:
    value = parent.get(key)
    if not isinstance(value, dict):
        raise RulesError(f"prompts: generation.yaml has no {key} mapping")
    return value


def _text(parent: dict, key: str) -> str:
    value = parent.get(key)
    if not isinstance(value, str) or not value.strip():
        raise RulesError(f"prompts: generation.yaml has no {key} text")
    return value.strip()


# context and knowledge are empty until Fase 9 and Fase 12, but Regla 1 declares three sections and the caller should see them.
def assemble(
    *,
    agent_name: str,
    components: Sequence[Component],
    message: str,
    context: str | None = None,
    knowledge: Sequence[str] = (),
) -> Prompt:
    spec = rules()

    blocks = [spec.preamble.replace(_AGENT_NAME, agent_name)]
    for component in components:
        # An empty component prints nothing, heading included: a blank section reads as a void the agent has to fill.
        if not component.content.strip():
            continue
        blocks.append(f"{spec.headings[component.type]}\n\n{component.content.strip()}")
    written = len(blocks) - 1
    blocks.append(spec.closing)

    if written < len(PromptComponentType):
        # Logged and not refused: F01 declares six, and an agent whose administrator has not written them all still answers.
        logger.info(
            "prompt assembled with fewer components than F01 declares",
            extra={"context": {"components": written, "declared": len(PromptComponentType)}},
        )

    return Prompt(
        system="\n\n".join(blocks),
        user=_envelope(spec, message, context, knowledge),
        rules_version=spec.version,
        components=written,
    )


# The order of Regla 1: what I know about this customer, then what my knowledge base says, then the literal question.
def _envelope(spec: Rules, message: str, context: str | None, knowledge: Sequence[str]) -> str:
    sections: list[tuple[str, str]] = []
    if context and context.strip():
        sections.append((spec.user_headings["context"], context.strip()))
    if knowledge:
        sections.append((spec.user_headings["knowledge"], _numbered(knowledge)))
    sections.append((spec.user_headings["message"], message.strip()))

    return "\n\n".join(f"{heading}\n{body}" for heading, body in sections)


# Numbered because retrieval_logs.rank is an order the model should see: the first fragment is the most similar one.
def _numbered(fragments: Sequence[str]) -> str:
    return "\n".join(f"{rank}. {text.strip()}" for rank, text in enumerate(fragments, start=1))
