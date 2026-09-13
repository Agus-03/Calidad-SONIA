# What a turn can run on, so the core refuses an agent configured with a model that would fail every turn (ADR-0038).

from pydantic import BaseModel


class PricedModel(BaseModel):
    provider: str
    model: str


# pricing_version travels so a refusal can name the tariff file that decided it (regla 9).
class ModelsResponse(BaseModel):
    pricing_version: int
    models: list[PricedModel]
