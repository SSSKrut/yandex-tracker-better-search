from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles
from pathlib import Path
from yandex_tracker_better_search.api.auth import router as auth_router
from yandex_tracker_better_search.api.search import router as search_router
from yandex_tracker_client import TrackerClient

client = TrackerClient(
    iam_token="t1.9euelZrNjcmQk5iPz86bx86SypuRlO3rnpWamJyOyIyWzZaTmZ2ZkpGJkMzl8_d2DU9B-e9oVB8G_d3z9zY8TEH572hUHwb9zef1656Vmpaaks-Ox8qdjMrHzJKKno6Q7_zF656Vmpaaks-Ox8qdjMrHzJKKno6Q.cq1lMUFVmKEttLacaWH0HhjrutHsccgk5QgZ2CwUxgmsTADZM0qdSAfrOTVbO8_7JPLZIoJuU3oRnP67j8D0Dw",
    cloud_org_id="bpfok5en3gnrdtjgdsvb",
)
issues = client.queues.get_all()[:3]
print([issue.key for issue in issues])
issues = client.issues.get_all()
print([issue.key for issue in issues])
for issue in issues:
    comments = list(issue.comments.get_all())[:3]
    print(f"{issue.key}: {[comment.id for comment in comments]}")


app = FastAPI()

app.include_router(auth_router, prefix="/auth")
app.include_router(search_router, prefix="/search")

FRONT_DIR = Path(__file__).parent.parent / "frontend"
app.mount("/", StaticFiles(directory=FRONT_DIR, html=True), name="static")
