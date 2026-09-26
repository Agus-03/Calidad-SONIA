# The first step of ADR-0016 and the cheapest: literal terms and no provider, so refusing a message costs nothing. One module for
# the two directions (regla 6), because what changes between the runs is the list and not the way of reading it.
#
# Two files feed it: blocklist.yaml is the policy of the business and profanity.yaml the words of decision O6b, read only on the
# way out. A forbidden term is a forbidden term, so both go through the same reading.

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

# Its own file because the two are loaded by different people, and whoever reads a blocked answer has to know which one refused it.
_PROFANITY_FILE = "profanity.yaml"

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
    # Which of the two files it came from, because detail_json is read by a person who then has to open one of them.
    file: str


@dataclass(frozen=True)
class Rules:
    version: int
    inbound: tuple[Term, ...]
    outbound: tuple[Term, ...]
    # Its own field and not merged into the one above, so what blocklist.yaml ships with can still be read on its own: decision
    # D4 keeps that file empty, and a merge would hide the day somebody writes a term of ours into it.
    profanity_version: int
    profanity: tuple[Term, ...]

    def terms(self, direction: MessageDirection) -> tuple[Term, ...]:
        if direction == MessageDirection.INBOUND:
            return self.inbound
        # The words of O6b last, so when both lists match it is the term of the business that gets reported.
        return self.outbound + self.profanity

    # varchar in the row, and what decided the outbound run is two files: naming only the first would let a word be loaded without
    # the version moving, and then every earlier row of this step would name a list that no longer exists — the same argument that
    # signal_evaluations.classifier_version exists for.
    def version_for(self, direction: MessageDirection) -> str:
        if direction == MessageDirection.INBOUND:
            return str(self.version)
        return f"{self.version}+profanity{self.profanity_version}"


# Read once per process: the files are versioned data, so they change with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    return _rules(load(_FILE), load(_PROFANITY_FILE))


# Both files are demanded, and a missing profanity.yaml is a load error and not an empty list: an empty list is a policy somebody
# chose, and a file that is not there is a deploy that lost it. O6b says this guard cannot be off.
def _rules(spec: RulesFile, profanity: RulesFile) -> Rules:
    return Rules(
        version=spec.version,
        inbound=_terms(spec.section("inbound"), _FILE),
        outbound=_terms(spec.section("outbound"), _FILE),
        profanity_version=profanity.version,
        # The whole file and not a section of it, which is the difference between a list somebody maintains and a policy.
        profanity=_terms(profanity, _PROFANITY_FILE),
    )


# Deduplicated by what is compared and not by what is written, because a list somebody keeps loading gets the same word twice and
# gets it in two spellings that fold to one: Puta and puta compile to the same pattern, and the second one could only make the
# file longer. The first spelling wins, since it is the one a blocked turn reports.
def _terms(section: RulesFile, file: str) -> tuple[Term, ...]:
    out: dict[str, Term] = {}
    for written in section.strings("terms"):
        normalized = _normalize(written)
        if not normalized:
            # Refused while loading: a pattern built out of nothing matches every message, so this one term would block the whole
            # business and every row of the trace would say the list had done its job.
            raise RulesFileError(f"blocklist: the term {written!r} is nothing once it is normalized")
        out.setdefault(normalized, Term(written=written, pattern=_pattern(normalized), file=file))
    return tuple(out.values())


# Whole word and not a substring, which is what keeps a short term from blocking half the conversation: casa must not fire on
# casamiento, and seña is an ordinary word in a rental. Lookarounds and not \b, because \b means something different depending on
# whether the term happens to begin with a letter or with a symbol.
def _pattern(normalized: str) -> re.Pattern[str]:
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
    # varchar in the row: for this step it is the version of the files, because a file is what decided and not a model (ADR-0016).
    version = spec.version_for(request.direction)
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
                        "file": term.file,
                    }
                },
            )
            # The term and the file that holds it, never the text it was found in: detail_json is evidence a person reads, two
            # lists feed the outbound run, and what the customer wrote has had no redaction at this point (ADR-0006).
            return StepResult(
                outcome=_BLOCKED,
                version=version,
                text=request.text,
                halts=True,
                detail={"term": term.written, "file": term.file},
            )

    # The text comes out exactly as it went in: this step decides and never rewrites, which is the guardrail's job and not this
    # one's. Nothing is logged, because a line per turn that says a list of terms did not match is noise.
    return StepResult(outcome=_PASS, version=version, text=request.text)
