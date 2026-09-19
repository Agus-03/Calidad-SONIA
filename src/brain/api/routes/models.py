# The priced models, read from rules/pricing.yaml: a model with no tariff fails every turn before calling the provider (ADR-0038).

from fastapi import APIRouter

from api.schemas.models import ModelsResponse, PricedModel
from pipeline.generation import pricing

router = APIRouter(tags=["models"])


# The same list rate_for() reads, so the core's check and the turn's refusal can never disagree about what is priced. A plain def
# and not async: the first call reads the file, and FastAPI runs a def in its thread pool instead of blocking the event loop.
@router.get("/models", response_model=ModelsResponse)
def list_models() -> ModelsResponse:
    spec = pricing()
    return ModelsResponse(
        pricing_version=spec.version,
        models=[PricedModel(provider=p, model=m) for p, m in sorted(spec.rates)],
    )
