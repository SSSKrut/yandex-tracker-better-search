from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles
from pathlib import Path
from yandex_tracker_better_search.api.auth import router as auth_router
from yandex_tracker_better_search.api.tasks import router as tasks_router

app = FastAPI()

app.include_router(auth_router, prefix="/auth")
app.include_router(tasks_router, prefix="/tasks")

FRONT_DIR = Path(__file__).parent.parent / "frontend"
app.mount("/", StaticFiles(directory=FRONT_DIR, html=True), name="static")
