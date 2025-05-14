from fastapi import APIRouter

router = APIRouter(prefix="", tags=["search"])


@router.get("/")
async def search(search_request: str, additional: str | None = None):
    task_id = search_request
    return {"task_id": additional, "status": task_id}
