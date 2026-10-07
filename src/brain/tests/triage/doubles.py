# The doubles of the triage step, shared by the tests of its verdict and of the signals it judges (ADR-0016, ADR-0045).

from datetime import UTC, datetime
from uuid import uuid4

from adapters.llm.base import Completion
from infra.enums import MessageDirection, PromptComponentType, SenderType
from pipeline import triage
from pipeline.repository.turn import Agent, Message, Turn
from pipeline.step import StepRequest, StepResult
from prompts.repository import Component

OBJECTIVE = "Alquilar y vender inmuebles en la ciudad de Córdoba."


# Records what it was asked, because half of what this step does is build the question: the objective has to reach the model or
# the verdict is about nothing.
class FakeClient:
    provider = "double"

    def __init__(self, answer: str) -> None:
        self.answer = answer
        self.calls = 0
        self.system: str | None = None
        self.messages: tuple = ()
        self.temperature: float | None = None

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        self.calls += 1
        self.system = system
        self.messages = messages
        self.temperature = temperature
        return Completion(text=self.answer, model=model, prompt_tokens=7, completion_tokens=3)


def turn(history: tuple[Message, ...] = ()) -> Turn:
    return Turn(
        conversation_id=uuid4(),
        message_batch_id=uuid4(),
        turn_number=1,
        agent=Agent(
            id=uuid4(),
            name="Agente demo",
            llm_provider="cohere",
            llm_model="command-a-03-2025",
            temperature=None,
            max_tokens=512,
        ),
        incoming=(
            Message(
                direction=MessageDirection.INBOUND,
                sender_type=SenderType.CONTACT,
                content_text="hola",
                sent_at=datetime.now(UTC),
            ),
        ),
        history=history,
    )


def said(text: str, direction: MessageDirection, sender: SenderType) -> Message:
    return Message(direction=direction, sender_type=sender, content_text=text, sent_at=datetime.now(UTC))


# Two of the six of F01, and the objective deliberately not first: the administrator orders them (Regla 1), so this step has to
# find it by type and never by position.
def components(objective: str | None = OBJECTIVE) -> tuple[Component, ...]:
    out = [Component(type=PromptComponentType.RULES, content="No tomar pagos por este canal.", position=1)]
    if objective is not None:
        out.append(Component(type=PromptComponentType.OBJECTIVE, content=objective, position=2))
    return tuple(out)


async def decide(
    answer: str,
    *,
    text: str = "¿tienen departamentos de dos dormitorios en Nueva Córdoba?",
    comps: tuple[Component, ...] | None = None,
    client: FakeClient | None = None,
    history: tuple[Message, ...] = (),
) -> StepResult:
    return await triage.run(
        StepRequest(
            turn=turn(history),
            client=client or FakeClient(answer),
            direction=MessageDirection.INBOUND,
            text=text,
            components=components() if comps is None else comps,
        )
    )
