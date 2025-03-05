from fastapi import APIRouter
from typing import Union

router = APIRouter(prefix="/auth", tags=["auth"])


@router.post("/")
def read_item():
    return {"Hello": "World"}
