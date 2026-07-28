# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Common commands

`Taskfile.yml` wraps everything below — `task --list` shows the tasks (`build`, `run`, `test`, `test:race`, `test:live`, `lint`, `fmt`, `up`, `down`, `ci`). On Arch the binary is `go-task`, because `task` belongs to Taskwarrior. The raw commands still work:

Build / run locally (requires a running Manticore at `MANTICORE_URL`, defaults to `http://localhost:9308`):

```bash
go build -o ytbs ./cmd/ytbs             # build the binary
go run ./cmd/ytbs serve                 # run the HTTP server (default :8080)
go run ./cmd/ytbs sync                  # one-shot sync from Yandex Tracker
go run ./cmd/ytbs search "query text"   # CLI search against the existing index
```

`serve` flags: `--addr` (default `:8080`), `--interval` (incremental sync, default `15m`), `--full-interval` (default `24h`). `sync` flags: `--queues q1,q2` and `--workers N`.

Tests:

```bash
go test ./...                                         # full suite
go test ./internal/indexer -run TestEscapeSQL_StripsControlAndEscapes  # single test
go test -run TestName ./pkg                           # single test by name

# search SQL against a live Manticore (skipped unless the env var is set)
MANTICORE_TEST_URL=http://localhost:9308 go test ./internal/indexer -run TestLive
```

Docker (brings up Manticore + the app, persisted volumes):

```bash
docker compose up --build     # UI on http://localhost:8080
```

Manticore data lives in the `manticore_data` Docker volume; backup/restore recipes are in `backups/README.md`. Sync state is persisted at `SYNC_STATE_PATH` (default `backups/sync_state.json`, mounted into the container by compose).

## Required environment

- `TRACKER_OAUTH_TOKEN` or `TRACKER_IAM_TOKEN` — auth for Yandex Tracker v3 API (one of them is required for sync/serve; IAM wins if both are set)
- `TRACKER_CLOUD_ORG_ID` — Cloud Organization ID, sent as `X-Cloud-Org-ID` (fatal if missing)
- `MANTICORE_URL` — Manticore HTTP API endpoint (default `http://localhost:9308`)

Optional tuning: `ATTACHMENT_TEXT_MAX_BYTES`, `SYNC_OVERLAP`, `SYNC_STATE_PATH`, `MAP_CACHE_MINUTES`, and `MAP_*` knobs read in `indexer.MapOptionsFromEnv`.

## Architecture

Layout: `cmd/<binary>/` holds the entrypoints (only `cmd/ytbs` today) and is the sole non-`internal` package tree; everything else lives under `internal/`. The repo root carries no Go code.

Entry point is `cmd/ytbs/main.go` → `cli.Execute()` (Cobra). `internal/cli/root.go` has a `PersistentPreRunE` that constructs the singleton `*indexer.Indexer` and a SIGINT/SIGTERM-cancellable `ctx` shared via `cli.GetContext()` / `GetIndexer()` / `GetTrackerToken()` / `GetTrackerOrgID()` — subcommands (`serve`, `sync`, `search`) read from these getters rather than rebuilding state.

Layers, in dependency order:

1. **`internal/tracker/`** — Yandex Tracker v3 API client (`baseURL = https://api.tracker.yandex.net/v3`). `fetch.go` pages issues/comments/attachments using `X-Scroll-Id` for full scans and `X-Total-Pages` for incrementals (issue search query `Updated: >= "..."`). `sync.go` orchestrates the worker-pool fetch (`InitialSync` / `IncrementalSync`) producing `IndexedIssue` (HTML stripped via regex; `URL = https://tracker.yandex.ru/<KEY>`) and `IndexedFile`. `attachments.go` decides which attachments to download as text (size ≤ `ATTACHMENT_TEXT_MAX_BYTES`, MIME `text/*` or extension allowlist) and decodes UTF-8 → Windows-1251 → KOI8-R fallbacks.
2. **`internal/indexer/`** — Manticore client wrapping the official Go SDK. `CreateTable` provisions two tables (`issues`, `files`) with `morphology='stem_en, stem_ru'`, `html_strip='1'`, and prefix/infix indexes on the searchable fields. Indexing uses `REPLACE INTO` with `escapeSQL` (drops NULs/control chars, escapes quotes/backslashes). Search runs a query expansion in `buildMatchVariants` — original token, prefix wildcard variant (`tok*`), infix variant (`*tok*`) — each variant parenthesised before being joined with `|`, because Manticore binds `|` tighter than the implicit AND. A link or issue key produces a second, attribute-only WHERE (`issue_key = ...` / `REGEX(url, ...)`) that runs as a separate query and is merged in Go: Manticore supports neither `LIKE` on string attributes nor `OR` between `MATCH()` and an attribute filter. Results from `issues` and `files` are merged and sorted by `updated_at DESC` (exact link/key hits first), capped at `limit`.
3. **`internal/indexer/map.go`** — separate "similarity map" pipeline (TF-IDF over docs → SVD via `gonum/mat` → 2D coords → k-means clustering → top-N cosine neighbors). Pure in-memory, no external services.
4. **`internal/sync/`** — periodic sync orchestrator. `Manager.Start` runs incremental + full tickers; on first boot (no `LastFullSyncAt`) it kicks off a full sync, otherwise an incremental. Manual triggers go through `requestChannel` (buffered size 1). Watermark tracked in `SyncState.LastUpdatedAt`, persisted to JSON via `StateStore`; incrementals query `since = LastUpdatedAt - SYNC_OVERLAP` to absorb clock skew.
5. **`internal/server/`** — `net/http` mux with htmx-driven HTML templates embedded via `//go:embed templates/*`. Pages: `/`, `/logs`, `/map`. APIs: `/api/search`, `/api/status`, `/api/sync` (POST=trigger, DELETE=cancel), `/api/map` (cached for `MAP_CACHE_MINUTES`, `?refresh=1` to bust). HTMX triggers are signalled via `HX-Trigger` headers (`sync-started`, `sync-cancelled`, `sync-error`).

## Conventions worth knowing

- The two Manticore tables (`issuesTableName`, `filesTableName`) and their infix-field lists are defined as constants at the top of `internal/indexer/indexer.go`. Schema changes need matching updates in `CreateTable`, `IndexIssues`/`IndexFiles`, and the search column lists in `searchIssuesWithFilters` / `searchFilesWithFilters`.
- `internal/server` depends on the unexported `searchService` interface, not on `*searchapi.Service` directly, so handlers are testable with a fake. A handler that starts using a new `Service` method must add it to that interface.
- IDs: when the upstream `id` is non-numeric, `hashString` derives a stable int64 (issues key off `Key`; files key off `IssueKey|FileName|FileURL`). Don't switch to a different hash without re-indexing.
- Release version stamping (`.goreleaser.yaml`) injects `-X .../internal/cli.version`; a wrong path is silently ignored by the linker, so keep it in sync when the CLI package moves.
- The CLI's `PersistentPreRunE` calls `log.Fatal` on missing env vars — adding new commands that don't need Tracker/Manticore would still trip this; gate with `cobra.Command.PersistentPreRunE` overrides if needed.
- Russian and English comments coexist in source; UI strings (`templates/*.html`, time-ago formatting in `internal/server/server.go`) are Russian.
- `.env` in the repo root may contain real credentials in some checkouts — never commit modifications to it; use `.env.example` for documentation.
