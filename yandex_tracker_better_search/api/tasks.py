from fastapi import APIRouter

router = APIRouter(prefix="/tasks", tags=["tasks"])


@router.post("/")
def get_tasks():
    return {"tasks": []}


@router.post("/{task_id}")
def get_task(task_id: str):
    return {"task_id": task_id, "status": "in_progress"}
