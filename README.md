# YTBS - Yandex Tracker Better Search

Удобный селф-хостед сервис поиска задач из вашего Яндекс Трекера. 

## Docker quick start

1. Создайте `.env` файл:

```
TRACKER_OAUTH_TOKEN=your_token
TRACKER_CLOUD_ORG_ID=your_org_id
```

2. Запустите контейнер:

```
docker compose up --build
```

UI будет доступен на `http://localhost:8080`.

## MCP server (для агентов)

`ytbs mcp` поднимает MCP-сервер по stdio с шестью тулами и ресурс-шаблоном
`tracker://issue/{key}`:

| Tool | Назначение |
|------|------------|
| `search_tasks` | full-text поиск с фильтрами (assignee, status, queue, ...) |
| `get_task` | полный issue + комменты + аттачи. `full=true` — без обрезки |
| `get_nearest_neighbors` | соседи в LSA-карте (похожие задачи) |
| `get_map_overview` | кластеры с top-keywords и центральными задачами |
| `trigger_sync` | запустить синк (`incremental`/`full`) |
| `get_status` | текущий статус и времена синков |

Подключить к Claude Code или другому MCP-клиенту — добавить в `~/.claude.json`
(или в per-project `.claude/mcp.json`):

```json
{
  "mcpServers": {
    "ytbs": {
      "command": "/path/to/ytbs",
      "args": ["mcp"],
      "env": { "MANTICORE_URL": "http://localhost:9308" }
    }
  }
}
```

`TRACKER_OAUTH_TOKEN` и `TRACKER_CLOUD_ORG_ID` нужны только если осуществляется вызов
`trigger_sync`; для read-only сценариев (например, когда агент смотрит задачи локально) их можно не передавать.
