# The one reader of rules/, which struct.md already places here: every file in that folder is versioned data (regla 9), and a
# second way of parsing one is a second way of being wrong about what decided a turn.

from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

# Anchored to the package and not to the working directory: the brain runs in a container, where neither is the repository.
RULES_DIR = Path(__file__).resolve().parent.parent / "rules"


# Raised while loading and never while answering: a rules file that cannot be read is a deployment mistake.
# Not named RulesError because prompts/assembler.py still owns that name for generation.yaml; the two meet when it migrates.
class RulesFileError(RuntimeError):
    pass


# Frozen because a rules file is data and not state, the same decision as the ports: what it says is fixed for the process.
@dataclass(frozen=True)
class RulesFile:
    # Carried so every message below names the file somebody has to fix, which a nested mapping cannot say on its own.
    name: str
    # Demanded of every file by regla 9: it is what pipeline_steps.version writes, and what lets a measurement name its prompt.
    version: int
    raw: dict[str, Any]

    # A nested mapping, as a file of its own: it keeps the version, because a section is part of that same file and not another.
    def section(self, key: str) -> "RulesFile":
        value = self.raw.get(key)
        if not isinstance(value, dict):
            raise RulesFileError(f"rules: {self.name} has no {key} mapping")
        return RulesFile(name=f"{self.name}:{key}", version=self.version, raw=value)

    # Stripped, because a prompt written as a YAML block ends in the newline the editor left and not in one anybody meant.
    def text(self, key: str) -> str:
        value = self.raw.get(key)
        if not isinstance(value, str) or not value.strip():
            raise RulesFileError(f"rules: {self.name} has no {key} text")
        return value.strip()

    def integer(self, key: str, *, minimum: int) -> int:
        return _integer(self.name, self.raw, key, minimum=minimum)


# Takes a name and not a path, so no caller repeats the walk up from its own __file__ to find the folder.
def load(name: str) -> RulesFile:
    return _load(RULES_DIR / name)


# Blocking on purpose, which section 2 of the conventions declares: the caller caches its own typed parse with lru_cache, so
# this runs once per process on the way up and never inside a turn.
def _load(path: Path) -> RulesFile:
    try:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as err:
        raise RulesFileError(f"rules: {path.name} cannot be read: {err}") from err
    if not isinstance(raw, dict):
        raise RulesFileError(f"rules: {path.name} is not a mapping")

    return RulesFile(
        name=path.name,
        version=_integer(path.name, raw, "version", minimum=1),
        raw=raw,
    )


# In one place because the version and every count share the same trap: in Python a bool is an int, so version: true would
# load, reach pipeline_steps.version as "True" and name a prompt nobody can find again.
def _integer(where: str, raw: dict[str, Any], key: str, *, minimum: int) -> int:
    value = raw.get(key)
    if not isinstance(value, int) or isinstance(value, bool) or value < minimum:
        raise RulesFileError(f"rules: {where} needs {key} as an integer of at least {minimum}")
    return value
