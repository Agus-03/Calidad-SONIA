from fastapi import APIRouter, Request, Response, status

router = APIRouter(tags=["health"])

# Answers 503 when the database is unreachable, because a check that always says ok cannot fail a deploy.
@router.get("/health")
async def health(request: Request, response: Response) -> dict[str, str]:
    if not await request.app.state.database.ping():
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return {"status": "unavailable"}
    return {"status": "ok"}
