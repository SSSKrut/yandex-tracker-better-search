import uvicorn
from yandex_tracker_better_search.backend.server import app


def start():
    uvicorn.run(
        "yandex_tracker_better_search.backend.server:app",
        host="0.0.0.0",
        port=7860,
        reload=True,
        ssl_keyfile="key.pem",
        ssl_certfile="cert.pem",
    )


if __name__ == "__main__":
    start()
