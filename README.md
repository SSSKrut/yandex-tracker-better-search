# YTBS - Yandex Tracker Better Search

<video src="https://private-user-images.githubusercontent.com/71554771/626851478-d0ba6cd5-5781-40f9-950a-9aa33752d832.mp4?jwt=eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJnaXRodWIuY29tIiwiYXVkIjoicmF3LmdpdGh1YnVzZXJjb250ZW50LmNvbSIsImtleSI6ImtleTUiLCJleHAiOjE3ODUxMDI4ODYsIm5iZiI6MTc4NTEwMjU4NiwicGF0aCI6Ii83MTU1NDc3MS82MjY4NTE0NzgtZDBiYTZjZDUtNTc4MS00MGY5LTk1MGEtOWFhMzM3NTJkODMyLm1wND9YLUFtei1BbGdvcml0aG09QVdTNC1ITUFDLVNIQTI1NiZYLUFtei1DcmVkZW50aWFsPUFLSUFWQ09EWUxTQTUzUFFLNFpBJTJGMjAyNjA3MjYlMkZ1cy1lYXN0LTElMkZzMyUyRmF3czRfcmVxdWVzdCZYLUFtei1EYXRlPTIwMjYwNzI2VDIxNDk0NlomWC1BbXotRXhwaXJlcz0zMDAmWC1BbXotU2lnbmF0dXJlPTMwYTNmNGZiM2ExMTExYjE3Y2NhZWFjZGJmY2I2MzQ4MTg2ZDkxZTk1YWMxMTI4ZjhiY2IxOTk0Y2NlN2NkZTYmWC1BbXotU2lnbmVkSGVhZGVycz1ob3N0JnJlc3BvbnNlLWNvbnRlbnQtdHlwZT12aWRlbyUyRm1wNCJ9.ZWsY8zor5bg93QcT6fpCAU2fwAWZfWuywdBNTXouUIk" controls width="100%"></video>

Удобный селф-хостед сервис поиска задач из вашего Яндекс Трекера.

Поддерживается два сценария: **локальный** (всё крутится у вас на машине)
и **командный** (один общий деплой в компании, остальные подключаются
по сети, без установки чего-либо у себя).

## Локальный запуск (для одного человека)

Подходит, если вы хотите пользоваться поиском сами или подключить MCP к своему
Claude Code / Cursor / другому клиенту со своей машины.

1. Создайте `.env` (за основу - `.env.example`):

   ```
   TRACKER_OAUTH_TOKEN=your_token
   TRACKER_CLOUD_ORG_ID=your_org_id
   ```

   `MCP_AUTH_TOKEN` оставьте пустым - на loopback эндпоинт всё равно открыт только
   для вас.

2. Запустите контейнеры:

   ```
   docker compose up --build
   ```

   - UI: <http://localhost:8080>
   - MCP HTTP: <http://localhost:8080/mcp>

3. Подключите MCP-клиент. Два варианта (выбирайте любой):

   **stdio** - клиент сам запускает `ytbs` как сабпроцесс:
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

   **HTTP** - клиент ходит на уже запущенный `ytbs serve`:
   ```json
   {
     "mcpServers": {
       "ytbs": {
         "url": "http://localhost:8080/mcp"
       }
     }
   }
   ```

## Командный запуск (один деплой на компанию)

Вариант, если вы хотите развернуть сервис где-то на сервере, и подключаться к нему по URL без локальных зависимостей, токена трекера и Manticore.

1. На сервере создайте `.env`:

   ```
   TRACKER_OAUTH_TOKEN=service_account_token
   TRACKER_CLOUD_ORG_ID=your_org_id

   # Обязательно: эндпоинт /mcp иначе будет открыт всем, кто видит порт 8080.
   MCP_AUTH_TOKEN=<любой_достаточно_длинный_секрет>
   ```

   Секрет можно сгенерировать, например, через `openssl rand -hex 32`.

2. Запустите `docker compose up -d --build`. Поставьте перед сервисом reverse-proxy
   с TLS (nginx/caddy/Traefik) - приложение слушает по HTTP.

3. Раздайте пользователям URL и токен. У каждого в конфиге MCP-клиента:

   ```json
   {
     "mcpServers": {
       "ytbs": {
         "url": "https://ytbs.company.internal/mcp",
         "headers": { "Authorization": "Bearer <тот_же_секрет>" }
       }
     }
   }
   ```

   Локально у пользователя ничего ставить не нужно.

## MCP-инструменты

`ytbs mcp` (stdio) и `/mcp` (HTTP) предоставляют один и тот же набор:

| Tool | Назначение |
|------|------------|
| `search_tasks` | full-text поиск с фильтрами (assignee, status, queue, ...) |
| `get_task` | полная задача + комменты + вложенные файлы. `full=true` - без обрезки |
| `get_nearest_neighbors` | соседи в LSA-карте (похожие задачи) |
| `get_map_overview` | кластеры с top-keywords и центральными задачами |
| `trigger_sync` | запустить синк (`incremental`/`full`) |
| `get_status` | текущий статус и времена синков |

Плюс ресурс-шаблон `tracker://issue/{key}`.

`TRACKER_OAUTH_TOKEN` и `TRACKER_CLOUD_ORG_ID` нужны только если клиент вызывает
`trigger_sync`. Для read-only сценариев их можно не передавать.

## Установка из релизов

В дополнение к `docker compose` можно скачать готовый бинарь с
[Releases](../../releases) — собран под `linux/darwin/windows × amd64/arm64`,
без зависимостей. Бинарю всё ещё нужен запущенный Manticore по `MANTICORE_URL`
(проще всего поднять через `docker compose up manticore`).

```sh
curl -L https://github.com/<owner>/<repo>/releases/latest/download/ytbs_<version>_linux_amd64.tar.gz | tar xz
./ytbs serve
```

## Переменные окружения

| Переменная | Назначение | Default |
|------------|------------|---------|
| `TRACKER_OAUTH_TOKEN` | OAuth-токен для Yandex Tracker v3 | - (нужен либо он, либо `TRACKER_IAM_TOKEN`) |
| `TRACKER_IAM_TOKEN` | IAM-токен (Bearer) от Yandex Cloud. Имеет приоритет над OAuth, если заданы оба | - |
| `TRACKER_CLOUD_ORG_ID` | Cloud Organization ID, шлётся как `X-Cloud-Org-ID` | - (нужен для `serve`/`sync`) |
| `MANTICORE_URL` | HTTP API Manticore | `http://localhost:9308` |
| `MCP_AUTH_TOKEN` | Bearer-токен для `/mcp`. Пусто = эндпоинт открыт | пусто |
| `SYNC_STATE_PATH` | где хранится стейт последнего синка | `backups/sync_state.json` |
| `ATTACHMENT_TEXT_MAX_BYTES` | лимит на размер скачиваемых текстовых файлов | `2097152` |


## Contribute

Тег вида `vX.Y.Z` в репозитории автоматически триггерит CI, который через goreleaser
собирает архивы и публикует их в Releases.
Все остальное - по ишус, с соблюдением гошных практик.
