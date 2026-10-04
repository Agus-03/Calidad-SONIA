# ADR-0067, reglas 5 a 9: what is described, with what, and what happens when the description fails.

import pytest

from adapters.llm.base import Completion, LLMError, LLMMessage
from adapters.llm.double import DESCRIPTION_MARKER, DoubleClient
from worker.images import Describer, rules, worth_describing
from worker.pdf import PageImage

IMAGE = PageImage(media_type="image/png", data=b"png", width=800, height=600)


class Recording:
    provider = "recording"

    def __init__(self, text: str = "Plano de un departamento de 2 ambientes, 48 m2.", error: Exception | None = None) -> None:
        self.text, self.error = text, error
        self.calls: list[dict[str, object]] = []

    async def complete(self, **kwargs: object) -> Completion:
        self.calls.append(kwargs)
        if self.error is not None:
            raise self.error
        return Completion(text=self.text, model=str(kwargs["model"]), prompt_tokens=1, completion_tokens=1)


def test_the_rules_name_sonnet_and_the_doubled_adapter_knows_the_prompt() -> None:
    described = rules()
    assert (described.provider, described.model) == ("anthropic", "claude-sonnet-5-5")
    assert described.system.startswith(DESCRIPTION_MARKER)


async def test_the_image_travels_with_the_prompt_of_the_rules() -> None:
    client = Recording()
    describer = Describer(client, rules())

    assert await describer.describe(IMAGE, page=3) == "Plano de un departamento de 2 ambientes, 48 m2."
    (call,) = client.calls
    (message,) = call["messages"]
    assert isinstance(message, LLMMessage) and message.images[0].data == b"png"
    assert call["system"] == rules().system and call["temperature"] == 0.0
    assert describer.version == rules().version


@pytest.mark.parametrize(
    "client",
    [
        Recording(error=LLMError("timeout")),
        Recording(text="  "),
        Recording(text='"Imagen sin información relevante."'),
        # Without its period, in other quotes or in another case: still the answer that says there is nothing.
        Recording(text="Imagen sin información relevante"),
        Recording(text="«imagen sin información relevante.»"),
    ],
)
async def test_a_failure_or_an_empty_image_leaves_nothing_on_the_page(client: Recording) -> None:
    assert await Describer(client, rules()).describe(IMAGE, page=1) is None


async def test_the_doubled_stack_describes_without_a_key() -> None:
    assert (await Describer(DoubleClient(), rules()).describe(IMAGE, page=1)).startswith("[doblado]")


def test_a_logo_is_not_described() -> None:
    described = rules()
    assert worth_describing(IMAGE, described)
    assert not worth_describing(PageImage("image/png", b"", described.min_width - 1, 900), described)
    assert not worth_describing(PageImage("image/png", b"", 900, described.min_height - 1), described)
