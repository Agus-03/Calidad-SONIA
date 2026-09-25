# The guardrail of ADR-0016, and it is not the redaction of ADR-0006: 03-capa-prompts regla 4 puts the redaction in a checkpoint
# of its own right after the batch closes —extract the datum, encrypt it, persist it, then substitute— and describes this step as
# "una segunda verificación de que no quedó ningún dato sensible sin redactar". This is the net under that checkpoint.
#
# The checkpoint does not exist yet, so today this net is the only thing substituting anything, and every message with a document
# number in it comes out of here as modified. That is a gap and not a design: regla 4 step 1 writes contact_attributes, which the
# brain is not allowed to write (regla 3), so whoever builds it has to settle that first.
#
# The two runs find the same thing and do opposite things with it, which is decision D3 of this phase. Inbound it replaces the
# datum and lets the turn go on (modified): a datum that was successfully redacted is no reason to leave a customer unanswered,
# and the provider never sees it either way. Outbound it refuses the answer whole (blocked) and never rewrites it, because that
# text has already been through the external provider — rewriting it would protect nothing ADR-0006 cares about and would only
# put a marker where the customer's own number was. So an inbound guardrail never cuts a turn, and an outbound one only ever cuts.

import logging
import re
from dataclasses import dataclass
from functools import lru_cache

from infra.enums import MessageDirection
from infra.rules import RulesFile, RulesFileError, load
from pipeline.step import StepRequest, StepResult

logger = logging.getLogger("sonia.pipeline.guardrail")

_FILE = "guardrails.yaml"

# The three values the domain of this step declares (DER/entidades.md), and each run uses two of them: pass and modified inbound,
# pass and blocked outbound. No single run of this step can produce all three.
_PASS = "pass"
_MODIFIED = "modified"
_BLOCKED = "blocked"


@dataclass(frozen=True)
class Redaction:
    # One of the four data of ADR-0006, and what the detail counts by. Two rules may share it: a DNI with dots and one without are
    # the same datum found two ways, not two findings.
    type: str
    marker: str
    pattern: re.Pattern[str]
    # The words that have to be in the message for this rule to hold at all, already compiled. Empty means it holds on its own.
    context: tuple[re.Pattern[str], ...]


@dataclass(frozen=True)
class Rules:
    version: int
    # In the order of the file, which is the order they are applied in: the longest number has to be replaced before a shorter
    # pattern gets to look at its digits.
    redactions: tuple[Redaction, ...]


# Read once per process: the file is versioned data, so it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    return _rules(load(_FILE))


def _rules(spec: RulesFile) -> Rules:
    redactions = tuple(_redaction(rule) for rule in spec.sections("redactions"))
    if not redactions:
        # Refused, unlike the empty blocklist: ADR-0006 declares four data, so a file with no rule in it is not a policy somebody
        # chose to leave empty — it is the mechanism of a foundational problem of the anteproyecto, missing.
        raise RulesFileError("guardrails: the file has no redaction, and ADR-0006 declares four")
    return Rules(version=spec.version, redactions=redactions)


def _redaction(rule: RulesFile) -> Redaction:
    marker = rule.text("marker")
    # A marker with a digit in it would be read by the rules that run after this one, so a CBU replaced by a marker could come
    # back out of here counted a second time as something else.
    if any(char.isdigit() for char in marker):
        raise RulesFileError(f"guardrails: the marker {marker!r} holds a digit, and the rules after it would read it")
    return Redaction(
        type=rule.text("type"),
        marker=marker,
        pattern=_compiled(rule, "pattern"),
        context=tuple(_compiled_word(rule, word) for word in rule.strings("requires_context")),
    )


def _compiled(rule: RulesFile, key: str) -> re.Pattern[str]:
    try:
        return re.compile(rule.text(key))
    except re.error as err:
        # On the way up and not on the turn that needed it: a pattern that does not compile is a deployment mistake.
        raise RulesFileError(f"guardrails: {rule.name} has a {key} that does not compile: {err}") from err


# Whole word, so a rule asking for "dni" is not satisfied by a longer word that happens to contain it.
def _compiled_word(rule: RulesFile, word: str) -> re.Pattern[str]:
    try:
        return re.compile(rf"(?<!\w){word}(?!\w)")
    except re.error as err:
        raise RulesFileError(f"guardrails: {rule.name} has a context word that does not compile: {err}") from err


# async with no I/O inside, like the blocklist: StepFunc is one shape for the seven, and three of them do call a provider.
async def run(request: StepRequest) -> StepResult:
    spec = rules()
    # varchar in the row, and for this step it is the file: a finding has to name the version of the patterns that produced it.
    version = str(spec.version)
    # One way of finding for the two runs, so the two directions can never disagree about what a personal datum is. The outbound
    # run throws the rewritten text away, and that is the whole of what D3 decided.
    redacted, counted = _redact(request.text, spec.redactions)

    if request.direction == MessageDirection.OUTBOUND:
        return _outbound(request, version, counted)
    return _inbound(request, version, redacted, counted)


def _inbound(request: StepRequest, version: str, redacted: str, counted: dict[str, int]) -> StepResult:
    if not counted:
        return StepResult(outcome=_PASS, version=version, text=redacted)

    # Warning and not info: every row this run writes means the checkpoint of regla 4 let a datum through, which is the false
    # negative ADR-0006 asks to measure. The types and how many, never a value — a log is not where a leak gets reopened.
    _report(request, "the guardrail redacted what reached it unredacted", counted)
    # modified and not blocked: the datum is gone, so the customer still gets an answer and the provider still never saw it.
    return StepResult(outcome=_MODIFIED, version=version, text=redacted, detail={"redacted": counted})


# The answer comes back out untouched, which is what the caller then writes into generations.completion_text: 07-secuencia 4.2
# keeps the blocked generation precisely so somebody can see what was stopped and with what prompt it was produced.
#
# The cost, and it is not hypothetical: an administrator who writes the agency own CBU into a prompt component so the AIAgent can
# pass it on gets every answer that repeats it refused. This step cannot tell the agency bank account from a customer one, and
# ADR-0006 is about the customer one.
def _outbound(request: StepRequest, version: str, counted: dict[str, int]) -> StepResult:
    if not counted:
        return StepResult(outcome=_PASS, version=version, text=request.text)

    _report(request, "the guardrail refused an answer that carried a personal datum", counted)
    return StepResult(
        outcome=_BLOCKED,
        version=version,
        text=request.text,
        halts=True,
        detail={"found": counted},
    )


def _report(request: StepRequest, message: str, counted: dict[str, int]) -> None:
    logger.warning(
        message,
        extra={
            "context": {
                "conversation_id": str(request.turn.conversation_id),
                "message_batch_id": str(request.turn.message_batch_id),
                "direction": request.direction,
                "found": counted,
            }
        },
    )


# Returns the text the next step decides about and how many of each datum were replaced, which is all the detail ever holds: a
# count can be audited and added up, and the value it counts is the one thing that must not be written down (ADR-0006).
def _redact(text: str, redactions: tuple[Redaction, ...]) -> tuple[str, dict[str, int]]:
    # Read off the text as it arrived, because no marker carries a digit and so no substitution can take a context word away.
    lowered = text.casefold()
    counted: dict[str, int] = {}

    for rule in redactions:
        if rule.context and not any(word.search(lowered) for word in rule.context):
            continue
        text, replaced = rule.pattern.subn(rule.marker, text)
        if replaced:
            counted[rule.type] = counted.get(rule.type, 0) + replaced

    return text, counted
