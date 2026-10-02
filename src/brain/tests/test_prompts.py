# The assembler of F03: the structure is fixed and the content is the agent's, which is what Regla 1 of 03-capa-prompts declares.

from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

import pytest

from infra.enums import PromptComponentType
from prompts.assembler import RulesError, _load, assemble, rules
from prompts.repository import Component, QuickReply, read_components, read_quick_replies

AGENT_ID = uuid4()

# One of each, in an order that is not the declaration order of the enumeration: what rules is position, not the code.
SIX = (
    Component(PromptComponentType.OBJECTIVE, "Ayudar a inquilinos.", 1, uuid4()),
    Component(PromptComponentType.PERSONALITY, "Cercano y resolutivo.", 2, uuid4()),
    Component(PromptComponentType.TONE, "Formal pero cálido.", 3, uuid4()),
    Component(PromptComponentType.BRAND_VOICE, "Inmobiliaria familiar.", 4, uuid4()),
    Component(PromptComponentType.BEHAVIOR, "Consultar antes de responder.", 5, uuid4()),
    Component(PromptComponentType.RULES, "No tomar pagos por este canal.", 6, uuid4()),
)


def build(components=SIX, **kwargs) -> object:
    return assemble(agent_name="Agente demo", components=components, message="hola", **kwargs)


# The six enter whole and in their position: OE1 measures that they are there, and the order belongs to the administrator.
def test_the_six_components_travel_in_the_order_of_their_position() -> None:
    prompt = build()

    positions = [prompt.system.index(c.content) for c in SIX]
    assert positions == sorted(positions), "the components were assembled out of their position"
    assert prompt.components == 6


# The frame is what makes it a dynamic prompt: six texts with no headings do not say where the tone ends and the rules begin.
def test_the_scaffolding_frames_every_component() -> None:
    prompt = build()
    spec = rules()

    for component in SIX:
        heading = spec.headings[component.type]
        assert heading in prompt.system, f"{component.type} went in without its heading"
        assert prompt.system.index(heading) < prompt.system.index(component.content)


# The name comes from ai_agents and not from a component, and it is stable per agent, so it does not break the caching.
def test_the_preamble_names_the_agent() -> None:
    assert "Agente demo" in build().system
    assert "{agent_name}" not in build().system


# The output format is a decision of the system: inside a component the administrator could delete it by accident.
def test_the_closing_is_always_there() -> None:
    prompt = build(components=())

    assert rules().closing in prompt.system
    assert prompt.components == 0


# F01 declares six, but a company with no brand voice is a valid state: the section disappears, heading included.
def test_a_missing_component_prints_no_section() -> None:
    without_voice = tuple(c for c in SIX if c.type != PromptComponentType.BRAND_VOICE)

    prompt = build(components=without_voice)

    assert rules().headings[PromptComponentType.BRAND_VOICE] not in prompt.system
    assert prompt.components == 5


# An empty component is the same case as an absent one: a blank section reads as a void the agent has to fill.
def test_an_empty_component_prints_no_section() -> None:
    blank = (*SIX[:5], Component(PromptComponentType.RULES, "   ", 6, uuid4()))

    prompt = build(components=blank)

    assert rules().headings[PromptComponentType.RULES] not in prompt.system
    assert prompt.components == 5


# What went in is what the generation names: a blank block left out of the text cannot be recorded as assembled (OE1).
def test_the_prompt_names_the_components_that_went_in_and_in_their_order() -> None:
    blank = (*SIX[:5], Component(PromptComponentType.RULES, "   ", 6, uuid4()))

    prompt = build(components=blank)

    assert [c.version_id for c in prompt.assembled] == [c.version_id for c in SIX[:5]]


# In Fase 1 there is neither dynamic context (Fase 9) nor RAG (Fase 12), so the envelope is the message and nothing else.
def test_the_envelope_carries_only_what_exists() -> None:
    prompt = build()
    spec = rules()

    assert prompt.user.startswith(spec.user_headings["message"])
    assert "hola" in prompt.user
    assert spec.user_headings["context"] not in prompt.user
    assert spec.user_headings["knowledge"] not in prompt.user


# The order of Regla 1: what I know about this customer, then what my knowledge base says, then the literal question.
def test_the_envelope_follows_the_order_of_regla_1() -> None:
    prompt = build(context="Intención: consulta_disponibilidad", knowledge=("Depto 2 ambientes.", "Requisitos."))
    spec = prompt_order = rules()

    positions = [
        prompt.user.index(spec.user_headings["context"]),
        prompt.user.index(spec.user_headings["knowledge"]),
        prompt.user.index(spec.user_headings["message"]),
    ]
    assert positions == sorted(positions), "the user envelope did not follow context, knowledge, message"
    # Numbered because retrieval_logs.rank is an order the model should see.
    assert "1. Depto 2 ambientes." in prompt.user
    assert "2. Requisitos." in prompt.user
    assert prompt_order is spec


# regla 9 exists so a measurement can name the version that produced it; a file without one cannot be cited.
def test_the_shipped_rules_declare_a_version_and_cover_the_six() -> None:
    spec = rules()

    assert isinstance(spec.version, int)
    assert set(spec.headings) == set(PromptComponentType)


# The six headings and the five of the envelope, so each case below breaks exactly one thing and never two.
_HEADINGS = "".join(f"    {t.value}: '#'\n" for t in PromptComponentType)
_ENVELOPE = "  headings:\n    context: c\n    knowledge: k\n    quick_replies: q\n    intents: i\n    message: m\n"
_USER = f"user:\n{_ENVELOPE}  quick_replies_limit: 30\n"
_SYSTEM = f"version: 1\nsystem:\n  preamble: hola\n  closing: chau\n  headings:\n{_HEADINGS}"


# A heading that is missing would drop a component out of the prompt in silence, which is what OE1 measures.
@pytest.mark.parametrize(
    ("body", "reason"),
    [
        ("- esto es una lista\n", "a file that is not a mapping"),
        (f"system: {{}}\n{_USER}", "no version"),
        (f"version: 1\n{_USER}", "no system mapping"),
        (
            f"version: 1\nsystem:\n  preamble: hola\n  closing: chau\n  headings:\n    objective: '#'\n{_USER}",
            "a heading missing for five of the six types",
        ),
        (
            f"version: 1\nsystem:\n  preamble: hola\n  closing: chau\n  headings:\n{_HEADINGS}"
            "user:\n  headings:\n    message: m\n",
            "the context and knowledge headings missing",
        ),
        (
            f"version: 1\nsystem:\n  preamble: ''\n  closing: chau\n  headings:\n{_HEADINGS}{_USER}",
            "an empty preamble",
        ),
        (f"{_SYSTEM}user:\n{_ENVELOPE}", "no ceiling for the quick replies"),
        (f"{_SYSTEM}user:\n{_ENVELOPE}  quick_replies_limit: 0\n", "a ceiling of zero"),
        (f"{_SYSTEM}user:\n{_ENVELOPE}  quick_replies_limit: true\n", "a ceiling that is a boolean"),
    ],
)
def test_a_scaffolding_that_cannot_serve_fails_on_load(tmp_path: Path, body: str, reason: str) -> None:
    broken = tmp_path / "generation.yaml"
    broken.write_text(body, encoding="utf-8")

    with pytest.raises(RulesError):
        _load(broken)


def test_a_scaffolding_that_is_not_there_fails_on_load(tmp_path: Path) -> None:
    with pytest.raises(RulesError):
        _load(tmp_path / "missing.yaml")


class FakeCursor:
    def __init__(self, rows: list[dict]) -> None:
        self._rows = rows

    async def fetchall(self) -> list[dict]:
        return self._rows

    async def fetchone(self) -> dict | None:
        return self._rows[0] if self._rows else None


# Answers the components query with its rows and the clock query alone with CLOCK, and keeps the components query in sql.
class FakeConnection:
    def __init__(self, rows: list[dict]) -> None:
        self.rows = rows
        self.sql = ""
        self.asked_the_clock = False

    async def execute(self, sql: str, params: dict | None = None) -> FakeCursor:
        if "prompt_components" not in sql:
            self.asked_the_clock = True
            return FakeCursor([{"read_at": CLOCK}])
        self.sql = sql
        return FakeCursor(self.rows)


CLOCK = datetime(2026, 9, 19, 12, 0, tzinfo=UTC)


# active is F01's ablation switch and deleted_at is the definitive removal (C09): the query has to filter both.
async def test_read_components_asks_for_the_active_ones_in_order() -> None:
    tone, rules_version = uuid4(), uuid4()
    connection = FakeConnection(
        [
            {"type": "tone", "content": "Formal.", "position": 3, "version_id": tone, "read_at": CLOCK},
            {"type": "rules", "content": "Sin pagos.", "position": 6, "version_id": rules_version, "read_at": CLOCK},
        ]
    )

    read = await read_components(connection, AGENT_ID)
    components = read.components

    assert [c.type for c in components] == [PromptComponentType.TONE, PromptComponentType.RULES]
    assert "c.active" in connection.sql
    assert "c.deleted_at IS NULL" in connection.sql
    assert "ORDER  BY c.position" in connection.sql
    # The newest version, which is the one the core wrote with this text (ADR-0039, regla 3).
    assert "ORDER  BY pv.version_number DESC" in connection.sql
    assert [c.version_id for c in components] == [tone, rules_version]
    # The instant of the same statement that read the texts, by the database's clock: what OE1 compares against (ADR-0039).
    assert "statement_timestamp()" in connection.sql
    assert read.read_at == CLOCK
    assert not connection.asked_the_clock, "the clock was asked apart although the read already carried it"


# No rows carry no instant, so the clock is asked on its own: a generation still needs its assembled_at.
async def test_read_components_returns_nothing_for_an_agent_with_no_components() -> None:
    connection = FakeConnection([])

    read = await read_components(connection, AGENT_ID)

    assert read.components == ()
    assert read.read_at == CLOCK
    assert connection.asked_the_clock


# The second half of F11: the labels the classifier read reach the model that answers, in the order the customer asked them.
def test_the_requests_of_f11_reach_the_prompt_in_order() -> None:
    prompt = build(intents=["consulta_disponibilidad", "consulta_precio"])
    heading = rules().user_headings["intents"]

    assert heading in prompt.user
    assert prompt.user.index("1. consulta_disponibilidad") < prompt.user.index("2. consulta_precio")
    # Right before the literal message, so the model has the list fresh when it writes the one answer that covers all of it.
    assert prompt.user.index(heading) < prompt.user.index(rules().user_headings["message"])


# Detecting them and not using them would leave F11 half done, so the closing has to tell the model to answer all of them.
def test_the_closing_asks_for_one_answer_that_covers_every_request() -> None:
    assert "sin dejar nada afuera" in build().system


# An empty list left in would tell the model the customer asked for nothing, which is the opposite of "we could not read it".
def test_a_batch_with_no_intents_prints_no_heading() -> None:
    prompt = build()

    assert rules().user_headings["intents"] not in prompt.user


# regla 9 exists so a measurement can name the version that produced it, and adding a section changed what gets produced.
def test_the_scaffolding_declares_the_version_that_carries_the_quick_replies() -> None:
    assert rules().version == 3


# A well-formed file loads, which is what keeps the cases above honest: each of them breaks one thing of this one.
def test_the_fixture_of_the_cases_above_loads(tmp_path: Path) -> None:
    good = tmp_path / "generation.yaml"
    good.write_text(f"{_SYSTEM}{_USER}", encoding="utf-8")

    assert _load(good).quick_replies_limit == 30


HOURS = QuickReply("Horarios", "Atendemos de 9 a 18.\nSábados de 9 a 13.")
PRICES = QuickReply("Precios", "La lista de precios va adjunta.")


# F07: the replies the admin loaded reach the model, each under its title and with the line breaks the admin wrote.
def test_the_quick_replies_reach_the_prompt_under_their_titles() -> None:
    prompt = build(quick_replies=(HOURS, PRICES))
    heading = rules().user_headings["quick_replies"]

    assert heading in prompt.user
    assert "1. Horarios\nAtendemos de 9 a 18.\nSábados de 9 a 13." in prompt.user
    assert prompt.user.index("1. Horarios") < prompt.user.index("2. Precios")


# The order of Regla 1 as ADR-0061 amends it: after the knowledge base and before what this batch asks.
def test_the_quick_replies_sit_between_the_knowledge_and_the_requests() -> None:
    prompt = build(knowledge=("Depto 2 ambientes.",), quick_replies=(HOURS,), intents=["consulta_precio"])
    spec = rules()

    positions = [prompt.user.index(spec.user_headings[name]) for name in ("knowledge", "quick_replies", "intents", "message")]
    assert positions == sorted(positions), "the quick replies left the place ADR-0061 gives them"


# An account with no catalogue prints no heading: an empty one would read as "the company has nothing for this".
def test_an_empty_catalogue_prints_no_heading() -> None:
    assert rules().user_headings["quick_replies"] not in build().user


# The list alone does not say what to do with it, so the fixed closing tells the model to adapt one and to skip the rest.
def test_the_closing_tells_the_model_how_to_use_the_quick_replies() -> None:
    system = build().system

    assert "respuestas rápidas" in system
    assert "no la nombrás" in system


# The system does not change: the replies are in the envelope, and the preview of ADR-0040 shows the system only.
def test_the_quick_replies_leave_the_system_untouched() -> None:
    assert build(quick_replies=(HOURS,)).system == build().system


# Answers the catalogue query with its rows and keeps what it was asked, so the filters and the ceiling can be asserted.
class CatalogueConnection:
    def __init__(self, rows: list[dict]) -> None:
        self.rows = rows
        self.sql = ""
        self.params: dict = {}

    async def execute(self, sql: str, params: dict | None = None) -> FakeCursor:
        self.sql, self.params = sql, params or {}
        return FakeCursor(self.rows)


# The operators' view of the catalogue (ADR-0060, regla 3): the agent's account, active and not deleted, by title.
async def test_read_quick_replies_asks_for_the_active_ones_of_the_account() -> None:
    connection = CatalogueConnection([{"title": "Horarios", "content": "De 9 a 18."}])

    replies = await read_quick_replies(connection, AGENT_ID, 30)

    assert replies == (QuickReply("Horarios", "De 9 a 18."),)
    assert "a.account_id = q.account_id" in connection.sql
    assert "q.active" in connection.sql
    assert "q.deleted_at IS NULL" in connection.sql
    assert "ORDER  BY lower(q.title)" in connection.sql
    # One more than the ceiling, which is how the read tells a catalogue that fits from one that was cut.
    assert connection.params == {"agent": AGENT_ID, "limit": 31}


# Over the ceiling, the first ones by title enter and the cut is logged, because the admin sees the whole catalogue.
async def test_read_quick_replies_keeps_the_ceiling_and_says_so(caplog: pytest.LogCaptureFixture) -> None:
    rows = [{"title": f"R{n}", "content": "texto"} for n in range(3)]

    with caplog.at_level("WARNING", logger="sonia.prompts.repository"):
        replies = await read_quick_replies(CatalogueConnection(rows), AGENT_ID, 2)

    assert [r.title for r in replies] == ["R0", "R1"]
    assert "over the ceiling" in caplog.text


# A catalogue that fits says nothing: a warning on every turn would bury the one that matters.
async def test_read_quick_replies_is_quiet_when_the_catalogue_fits(caplog: pytest.LogCaptureFixture) -> None:
    with caplog.at_level("WARNING", logger="sonia.prompts.repository"):
        replies = await read_quick_replies(CatalogueConnection([{"title": "R0", "content": "texto"}]), AGENT_ID, 2)

    assert len(replies) == 1
    assert not caplog.records
