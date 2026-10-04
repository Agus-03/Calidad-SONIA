# The description of a document's images (ADR-0067, reglas 5 a 9): Claude Sonnet 5.5 through the port of adapters/llm, with the
# prompt in rules/image_description.yaml. An image that fails is left out, and the document is ingested all the same.

import logging
from dataclasses import dataclass
from functools import lru_cache

from adapters.llm.base import LLMClient, LLMError, LLMImage, LLMMessage
from infra import rules as rules_file
from worker.pdf import PageImage

logger = logging.getLogger("sonia.worker.images")


@dataclass(frozen=True)
class DescriptionRules:
    # What document_chunks.description_prompt_version stores (ADR-0065, regla 9).
    version: int
    provider: str
    model: str
    max_tokens: int
    min_width: int
    min_height: int
    max_side: int
    system: str
    user: str
    nothing: str


@lru_cache(maxsize=1)
def rules() -> DescriptionRules:
    loaded = rules_file.load("image_description.yaml")
    return DescriptionRules(
        version=loaded.version,
        provider=loaded.text("provider"),
        model=loaded.text("model"),
        max_tokens=loaded.integer("max_tokens", minimum=1),
        min_width=loaded.integer("min_width", minimum=1),
        min_height=loaded.integer("min_height", minimum=1),
        max_side=loaded.integer("max_side", minimum=1),
        system=loaded.text("system"),
        user=loaded.text("user"),
        nothing=loaded.text("nothing"),
    )


# A logo or an icon is not described (ADR-0067, regla 6): either side under its minimum is enough to leave it out.
def worth_describing(image: PageImage, described: DescriptionRules) -> bool:
    return image.width >= described.min_width and image.height >= described.min_height


class Describer:
    def __init__(self, client: LLMClient, described: DescriptionRules) -> None:
        self._client = client
        self._rules = described

    @property
    def version(self) -> int:
        return self._rules.version

    # None for an image that failed or that the model found empty: the page keeps its text and nothing else (ADR-0067, regla 9).
    async def describe(self, image: PageImage, *, page: int) -> str | None:
        message = LLMMessage(role="user", text=self._rules.user, images=(LLMImage(image.media_type, image.data),))
        try:
            completion = await self._client.complete(
                model=self._rules.model,
                system=self._rules.system,
                messages=(message,),
                temperature=0.0,
                max_tokens=self._rules.max_tokens,
            )
        except LLMError as err:
            logger.warning("image not described", extra={"context": {"page": page, "error": str(err)}})
            return None
        text = completion.text.strip()
        if not text or _plain(text) == _plain(self._rules.nothing):
            return None
        return text


# The answer the prompt asks for when there is nothing to describe, recognised without its quotes, its final period or its case:
# a model that drops the period would otherwise put "[Imagen: Imagen sin información relevante]" into a chunk.
def _plain(text: str) -> str:
    return text.strip().strip("\"'«»“”").strip().rstrip(".").strip().casefold()
