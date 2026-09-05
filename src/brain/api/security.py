# Authentication between the two deployables: a shared secret in X-Internal-Token (ADR-0017 (k)).

import secrets
from typing import Annotated

from fastapi import Header, HTTPException, Request, status


# Rejects a caller that is not the core, comparing in constant time so a wrong token leaks nothing through timing.
async def require_internal_token(
    request: Request,
    x_internal_token: Annotated[str, Header()] = "",
) -> None:
    expected: str = request.app.state.settings.brain_internal_token
    if not secrets.compare_digest(x_internal_token, expected):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail={
                "code": "invalid_internal_token",
                "message": "the X-Internal-Token header is missing or does not match",
            },
        )
