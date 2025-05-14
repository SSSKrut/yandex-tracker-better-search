from fastapi import APIRouter
from typing import Union
from pydantic import BaseModel

router = APIRouter(prefix="", tags=["auth"])


class AuthRequest(BaseModel):
    oauth_token: str
    organization_id: str


@router.post("/")
async def read_auth(request: AuthRequest):
    print(request)
    return {"result": "ok"}
