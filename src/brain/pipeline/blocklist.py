# The first step of ADR-0016 and the cheapest of the seven: literal terms, no provider, so a message with a forbidden word costs
# nothing to refuse. It is also the first one that can end a turn, which is why the whole cut of 07-secuencia section 4.1 can be
# exercised without a model answering anything.
#
# One module for the two directions (regla 6), because what changes between the inbound run and the outbound one is the list and
# not the way of reading it.

import logging
import re
import unicodedata
from dataclasses import dataclass
from functools import lru_cache

from infra.enums import MessageDirection
from infra.rules import RulesFile, RulesFileError, load
from pipeline.step import StepRequest, StepResult

logger = logging.getLogger("sonia.pipeline.blocklist")

_FILE = "blocklist.yaml"

# The outcome domain declared for this step in DER/entidades.md: it lets a text through or it ends the turn. There is no third
# value, and in particular no failed — a list cannot be unavailable.
_PASS = "pass"
_BLOCKED = "blocked"


# The written form is kept next to the pattern because it is what the detail reports: whoever reads a blocked turn has to be able
# to find the line of the file that caused it, and the normalized form is not what anybody wrote.
@dataclass(frozen=True)
class Term:
    written: str
    pattern: re.Pattern[str]


@dataclass(frozen=True)
class Rules:
    version: int
    inbound: tuple[Term, ...]
    outbound: tuple[Term, ...]

    def terms(self, direction: MessageDirection) -> tuple[Term, ...]:
        return self.inbound if direction == MessageDirection.INBOUND else self.outbound


# Read once per process: the file is versioned data, so it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    return _rules(load(_FILE))


def _rules(spec: RulesFile) -> Rules:
    return Rules(
        version=spec.version,
        inbound=_terms(spec.section("inbound")),
        outbound=_terms(spec.section("outbound")),
    )


def _terms(section: RulesFile) -> tuple[Term, ...]:
    return tuple(Term(written=term, pattern=_pattern(term)) for term in section.strings("terms"))


# Whole word and not a substring, which is what keeps a short term from blocking half the conversation: casa must not fire on
# casamiento, and seña is an ordinary word in a rental. Lookarounds and not \b, because \b means something different depending on
# whether the term happens to begin with a letter or with a symbol.
def _pattern(term: str) -> re.Pattern[str]:
    normalized = _normalize(term)
    if not normalized:
        # Refused while loading: a pattern built out of nothing matches every message, so this file would block the business.
        raise RulesFileError(f"blocklist: the term {term!r} is nothing once it is normalized")
    return re.compile(rf"(?<!\w){re.escape(normalized)}(?!\w)")


# Case and accents folded on both sides, because a customer writes the way they write and a list that has to be kept in every
# spelling is a list that does not block. The cost, on purpose: dropping the combining marks makes ñ and n the same letter, so
# term cannot distinguish two words that differ only by an accent.
def _normalize(text: str) -> str:
    decomposed = unicodedata.normalize("NFD", text.casefold())
    return "".join(char for char in decomposed if not unicodedata.combining(char))


# async with no I/O inside, which section 2 of the conventions argues against on its own: StepFunc is one shape for the seven and
# three of them do call a provider, so the cheap steps pay one await rather than the runner branching on which kind it holds.
async def run(request: StepRequest) -> StepResult:
    spec = rules()
    # varchar in the row: for this step it is the version of the file, because a file is what decided and not a model (ADR-0016).
    version = str(spec.version)
    # Normalized once and not per term: the comparison is the same for all of them.
    text = _normalize(request.text)

    for term in spec.terms(request.direction):
        if term.pattern.search(text):
            logger.info(
                "the blocklist ended the turn",
                extra={
                    "context": {
                        "conversation_id": str(request.turn.conversation_id),
                        "message_batch_id": str(request.turn.message_batch_id),
                        "direction": request.direction,
                        "term": term.written,
                    }
                },
            )
            # The term and never the text it was found in: detail_json is evidence a person reads, and what the customer wrote
            # has had no redaction at this point in the turn (ADR-0006).
            return StepResult(
                outcome=_BLOCKED,
                version=version,
                text=request.text,
                halts=True,
                detail={"term": term.written},
            )

    # The text comes out exactly as it went in: this step decides and never rewrites, which is the guardrail's job and not this
    # one's. Nothing is logged, because a line per turn that says a list of terms did not match is noise.
    return StepResult(outcome=_PASS, version=version, text=request.text)
