# The assembler of F03: the structure is fixed and the content is the agent's, which is what Regla 1 of 03-capa-prompts declares.

from pathlib import Path
from uuid import uuid4

import pytest

from infra.enums import PromptComponentType
from prompts.assembler import RulesError, _load, assemble, rules
from prompts.repository import Component, read_components

AGENT_ID = uuid4()

# One of each, in an order that is not the declaration order of the enumeration: what rules is position, not the code.
SIX = (
    Component(PromptComponentType.OBJECTIVE, "Ayudar a inquilinos.", 1),
    Component(PromptComponentType.PERSONALITY, "Cercano y resolutivo.", 2),
    Component(PromptComponentType.TONE, "Formal pero cálido.", 3),
    Component(PromptComponentType.BRAND_VOICE, "Inmobiliaria familiar.", 4),
    Component(PromptComponentType.BEHAVIOR, "Consultar antes de responder.", 5),
    Component(PromptComponentType.RULES, "No tomar pagos por este canal.", 6),
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
    blank = (*SIX[:5], Component(PromptComponentType.RULES, "   ", 6))

    prompt = build(components=blank)

    assert rules().headings[PromptComponentType.RULES] not in prompt.system
    assert prompt.components == 5


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


# The six headings and the three of the envelope, so each case below breaks exactly one thing and never two.
_HEADINGS = "".join(f"    {t.value}: '#'\n" for t in PromptComponentType)
_USER = "user:\n  headings:\n    context: c\n    knowledge: k\n    message: m\n"


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


class FakeConnection:
    def __init__(self, rows: list[dict]) -> None:
        self.rows = rows
        self.sql = ""

    async def execute(self, sql: str, params: dict | None = None) -> FakeCursor:
        self.sql = sql
        return FakeCursor(self.rows)


# active is F01's ablation switch and deleted_at is the definitive removal (C09): the query has to filter both.
async def test_read_components_asks_for_the_active_ones_in_order() -> None:
    connection = FakeConnection(
        [{"type": "tone", "content": "Formal.", "position": 3}, {"type": "rules", "content": "Sin pagos.", "position": 6}]
    )

    components = await read_components(connection, AGENT_ID)

    assert [c.type for c in components] == [PromptComponentType.TONE, PromptComponentType.RULES]
    assert "c.active" in connection.sql
    assert "c.deleted_at IS NULL" in connection.sql
    assert "ORDER  BY c.position" in connection.sql


async def test_read_components_returns_nothing_for_an_agent_with_no_components() -> None:
    assert await read_components(FakeConnection([]), AGENT_ID) == ()
