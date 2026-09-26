# The fixed answer of decision O2: which text the customer gets when the pipeline refused the turn. Not a step and not part of the
# trace — it only reads what the step that cut already decided. It lives here and not in api/ because it is keyed by a cut point
# of ADR-0016; the brain proposes the text and the core writes the message (07-secuencia 0).

from dataclasses import dataclass
from functools import lru_cache

from infra.enums import MessageDirection, PipelineStep
from infra.rules import RulesFile, RulesFileError, load

_FILE = "notices.yaml"

# The cut that says nothing (decision O1c): a question outside the domain gets no answer at all, only a person. Enforced here and
# not left to the file, because it is a decision of the design and not a knob of the business — the day it changes, this line and
# the decision move together.
_SILENT = PipelineStep.TRIAGE


@dataclass(frozen=True)
class Notice:
    direction: MessageDirection
    step: PipelineStep
    text: str


@dataclass(frozen=True)
class Rules:
    version: int
    notices: tuple[Notice, ...]

    # None is silence, and it is what O1c asks for on the triage. A step with no text says nothing rather than inventing something,
    # which is also what a step that starts cutting in Fase 12 will do until somebody writes its entry (declared in the file).
    def text_for(self, step: PipelineStep, direction: MessageDirection) -> str | None:
        for notice in self.notices:
            if notice.step == step and notice.direction == direction:
                return notice.text
        return None


# Read once per process, like every other file of rules/: it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    return _rules(load(_FILE))


def _rules(spec: RulesFile) -> Rules:
    notices: list[Notice] = []
    for direction in MessageDirection:
        # Both legs demanded, even the one with a single entry: a leg that is missing is a file half written, and the two are told
        # apart by the section being there — the same reading blocklist.yaml gets.
        section = spec.section(direction)
        for key in section.raw:
            notices.append(Notice(direction=direction, step=_step(section, key), text=section.text(key)))
    return Rules(version=spec.version, notices=tuple(notices))


# A key that is not a step of ADR-0016 is refused while loading and not read as silence: a typo would otherwise ship as a cut point
# that answers nothing, which is exactly what a correct triage entry looks like.
def _step(section: RulesFile, key: object) -> PipelineStep:
    try:
        step = PipelineStep(key)
    except ValueError as err:
        raise RulesFileError(f"notices: {section.name} has {key!r}, which is not a step of the pipeline") from err
    if step == _SILENT:
        raise RulesFileError(
            f"notices: {section.name} has a text for the triage, and decision O1c says a question outside the domain "
            "is answered by a person and never by a text of ours"
        )
    return step
