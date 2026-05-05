# YTBS - Yandex Tracker Better Search

Self-hosted caching search service for your Yandex Tracker tasks.

## Docker quick start

1. Create a `.env` file with your credentials:

```
TRACKER_OAUTH_TOKEN=your_token
TRACKER_CLOUD_ORG_ID=your_org_id
```

2. Run:

```
docker compose up --build
```

The UI будет доступен на `http://localhost:8080`.
