# F04: the dynamic context of the conversation, rewritten once per batch that reaches generation and injected into the prompt
# of the answer (03-capa-prompts, Regla 3; ADR-0048).

import json
import time
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, replace
from functools import lru_cache

from adapters.llm.base import LLMClient, LLMMessage, LLMRefusal, unfenced
from classifiers.intent import Intent
from infra.rules import RulesFileError, load
from pipeline.repository.context import Context, content_of

_FILE = "rewriter.yaml"

# Enough of a bad answer to recognise it in a log; the text it came from was redacted before the triage (ADR-0006).
_MAX_REPORTED = 200

# varchar with no limit in the schema, but a budget signal is a few words: anything longer is a paragraph the model wrote instead.
_MAX_BUDGET = 60

# The same bound for one objection, which the prompt asks for "en menos de diez palabras".
_MAX_OBJECTION = 120

# Where the two caps go in the text, so the model reads the numbers the parser enforces and not a copy that drifts from them.
_MAX_OBJECTIONS = "{max_objections}"
_URGENCY_MAX = "{urgency_max}"

# The lines of the rendered context, in the order they are read: what the customer wants before what stands in the way.
_RENDERED = ("summary", "primary_intent", "objections", "urgency_level", "budget_signal")


@dataclass(frozen=True)
class Rules:
    version: int
    marker: str
    system: str
    previous_heading: str
    user_heading: str
    max_objections: int
    urgency_max: int
    labels: Mapping[str, str]


# Read once per process: the file is versioned data, so it changes with a deploy and never between two turns.
@lru_cache(maxsize=1)
def rules() -> Rules:
    spec = load(_FILE)
    marker = spec.text("marker")
    system = spec.text("system")
    # Checked while loading, because the doubled adapter finds this call by that literal: without it the doubled stack would
    # answer the rewrite with prose, every rewrite would fail, and the fallback would hide it.
    if marker not in system:
        raise RulesFileError(f"rewriter: the system prompt does not carry the marker {marker!r}")
    # The same reason for the caps: a prompt that lost one would ask for a scale the parser no longer enforces.
    for placeholder in (_MAX_OBJECTIONS, _URGENCY_MAX):
        if placeholder not in system:
            raise RulesFileError(f"rewriter: the system prompt does not carry {placeholder}")

    max_objections = spec.integer("max_objections", minimum=1)
    urgency_max = spec.integer("urgency_max", minimum=1)
    render = spec.section("render")
    return Rules(
        version=spec.version,
        marker=marker,
        system=_filled(system, max_objections, urgency_max),
        previous_heading=spec.text("previous"),
        user_heading=spec.text("user"),
        max_objections=max_objections,
        urgency_max=urgency_max,
        labels={key: _filled(render.text(key), max_objections, urgency_max) for key in _RENDERED},
    )


# replace and not format: the prompt carries a JSON example whose braces format would read as fields.
def _filled(text: str, max_objections: int, urgency_max: int) -> str:
    return text.replace(_MAX_OBJECTIONS, str(max_objections)).replace(_URGENCY_MAX, str(urgency_max))


# One rewrite and what it cost, which is what conversation_context_versions keeps besides the context itself.
@dataclass(frozen=True)
class Rewrite:
    context: Context
    model: str
    latency_ms: int


# Takes no connection: the call takes seconds, and holding a pool connection through it starves the next turn. It raises
# LLMError on anything it cannot use, and the caller falls back to the last context (Regla 3).
async def rewrite(*, client: LLMClient, model: str, previous: Context | None, message: str) -> Rewrite:
    spec = rules()

    user = f"{spec.user_heading}\n{message.strip()}"
    if previous is not None:
        # Before the message, in the order the conversation happened; as the same JSON the model returns, so it reads its own shape.
        user = f"{spec.previous_heading}\n{json.dumps(_prompted(previous), ensure_ascii=False)}\n\n{user}"

    # perf_counter and not the wall clock: latency_ms is a duration, and the wall clock can step backwards mid-turn.
    started = time.perf_counter()
    completion = await client.complete(
        model=model,
        system=spec.system,
        messages=(LLMMessage(role="user", text=user),),
        # Zero so the same batch rewrites the same way twice: OE4 compares this against annotations (ADR-0023).
        temperature=0.0,
    )
    latency_ms = round((time.perf_counter() - started) * 1000)

    context = parse(completion.text, spec)
    if previous is not None:
        # Neither is the rewriter's to decide: the intent comes from F11 and the extracted data from F31 (ADR-0048).
        context = replace(context, primary_intent=previous.primary_intent, extracted_data=previous.extracted_data)
    return Rewrite(context=context, model=completion.model, latency_ms=latency_ms)


# Separate from the call so every case below can be tested without a model. A malformed field is dropped and not fatal, but an
# answer that is not the object at all is a refusal: writing an empty context would erase the one that was there.
def parse(answer: str, spec: Rules) -> Context:
    text = unfenced(answer)
    try:
        payload = json.loads(text)
    except ValueError as err:
        raise LLMRefusal(f"rewriter: the model did not answer JSON: {text[:_MAX_REPORTED]}") from err
    if not isinstance(payload, dict):
        raise LLMRefusal(f"rewriter: the model answered JSON that is not an object: {text[:_MAX_REPORTED]}")

    summary = payload.get("summary")
    if not isinstance(summary, str) or not summary.strip():
        raise LLMRefusal("rewriter: the answer has no summary, and a context without one is not a rewrite")

    return Context(
        summary=summary.strip(),
        primary_intent=None,
        objections=_objections(payload.get("objections"), spec.max_objections),
        urgency_level=_urgency(payload.get("urgency_level"), spec.urgency_max),
        budget_signal=_short(payload.get("budget_signal"), _MAX_BUDGET),
        intent_changed=payload.get("intent_changed") if isinstance(payload.get("intent_changed"), bool) else None,
    )


# The dominant intention of the batch, which is how DER/entidades.md defines primary_intent: the highest confidence, and the one
# said first when two tie. None when F11 read nothing, and the caller keeps the one there was.
def primary(intents: Sequence[Intent]) -> str | None:
    if not intents:
        return None
    return min(intents, key=lambda i: (-i.confidence, i.position)).label


# The section the assembler prints under [CONTEXTO DE LA CONVERSACIÓN]. An empty value prints no line, the same as an empty
# section (Regla 1): a line that says "Presupuesto:" and nothing tells the model we asked and the customer answered nothing.
def render(context: Context) -> str:
    spec = rules()
    values = {
        "summary": context.summary,
        "primary_intent": context.primary_intent,
        "objections": "; ".join(context.objections),
        "urgency_level": str(context.urgency_level) if context.urgency_level is not None else None,
        "budget_signal": context.budget_signal,
    }
    return "\n".join(f"{spec.labels[key]} {values[key]}" for key in _RENDERED if values[key])


# The context as the model is asked to return it, so what it reads and what it writes have one shape.
def _prompted(context: Context) -> dict:
    content = content_of(context)
    return {
        "summary": content["summary"],
        "objections": content["objections_json"],
        "urgency_level": content["urgency_level"],
        "budget_signal": content["budget_signal"],
    }


# The most recent when there are too many: the model appends what the customer just said, so the first ones are the oldest and the
# last one is the objection of this batch (ADR-0048).
def _objections(value: object, limit: int) -> tuple[str, ...]:
    if not isinstance(value, list):
        return ()
    out: list[str] = []
    for item in value:
        text = _short(item, _MAX_OBJECTION)
        # The same objection twice is one objection, which the prompt asks the model not to repeat.
        if text is not None and text not in out:
            out.append(text)
    return tuple(out[-limit:])


# Out of the scale is not judged, the same reading as a flag that is not a bool: writing it would put a number nobody asked for.
def _urgency(value: object, top: int) -> int | None:
    if not isinstance(value, int) or isinstance(value, bool) or not 1 <= value <= top:
        return None
    return value


# Cut and not dropped: a budget that came out too long is still the budget, and null would read as "the customer said nothing".
# At the last space within the limit, so the stored text does not end halfway through a word.
def _short(value: object, limit: int) -> str | None:
    if not isinstance(value, str) or not value.strip():
        return None
    text = value.strip()
    if len(text) <= limit:
        return text
    cut = text[:limit]
    return cut[: cut.rfind(" ")].rstrip() if " " in cut else cut
