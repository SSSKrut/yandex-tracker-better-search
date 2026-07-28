package server

import (
	"context"
	"embed"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/SSSKrut/yandex-tracker-better-search/internal/indexer"
	"github.com/SSSKrut/yandex-tracker-better-search/internal/searchapi"
	syncer "github.com/SSSKrut/yandex-tracker-better-search/internal/sync"
)

//go:embed templates/*
var templatesFS embed.FS

// searchService - то, что ручки используют из searchapi.Service. Интерфейс
// объявлен на стороне потребителя, чтобы их можно было тестировать без Manticore.
type searchService interface {
	Status() searchapi.FullStatus
	Logs(limit int) []syncer.LogEntry
	GetFilterOptions(ctx context.Context) (*searchapi.FilterOptions, error)
	SearchRich(ctx context.Context, p searchapi.SearchParams) ([]searchapi.IndexerSearchResult, error)
	Map(ctx context.Context, refresh bool) (*indexer.MapData, error)
	TriggerSync(mode string) error
	CancelSync() error
}

// Server - HTTP server
type Server struct {
	api        searchService
	templates  *template.Template
	addr       string
	mcpHandler http.Handler
}

// NewServer - creates a new Server instance. If mcpHandler is non-nil, it is
// mounted at /mcp (and /mcp/) so MCP clients can connect over HTTP.
func NewServer(addr string, api searchService, mcpHandler http.Handler) (*Server, error) {

	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "никогда"
			}
			return t.Format("02.01.2006 15:04:05")
		},
		"timeAgo": func(t time.Time) string {
			if t.IsZero() {
				return "никогда"
			}
			d := time.Since(t)
			switch {
			case d < time.Minute:
				return "только что"
			case d < time.Hour:
				return formatDuration(d.Minutes(), "минуту", "минуты", "минут") + " назад"
			case d < 24*time.Hour:
				return formatDuration(d.Hours(), "час", "часа", "часов") + " назад"
			default:
				return t.Format("02.01.2006 15:04")
			}
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"progressPercent":    progressPercent,
		"progressStageLabel": progressStageLabel,
		"sub100":             func(n int) int { return 100 - n },
	}).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Server{
		api:        api,
		templates:  tmpl,
		addr:       addr,
		mcpHandler: mcpHandler,
	}, nil
}

// progressPercent maps a Progress event to a 0-100 integer for the circular
// indicator. Returns 0 when the total is unknown so the ring shows an empty
// (indeterminate-looking) state, and clamps the upper bound to 100.
func progressPercent(p syncer.Progress) int {
	if p.Total <= 0 || p.Current <= 0 {
		return 0
	}
	pct := p.Current * 100 / p.Total
	if pct > 100 {
		return 100
	}
	return pct
}

// progressStageLabel renders a Russian label for the current sync stage.
func progressStageLabel(stage string) string {
	switch stage {
	case syncer.StageIssues:
		return "Загружаем задачи"
	case syncer.StageComments:
		return "Комментарии и файлы"
	case syncer.StageIndexing:
		return "Индексация"
	default:
		return "Синхронизация"
	}
}

// formatDuration - число со склонённой единицей: "1 минуту", "45 минут".
func formatDuration(n float64, one, few, many string) string {
	i := int(n)
	return strconv.Itoa(i) + " " + pluralForm(i, one, few, many)
}

func pluralForm(i int, one, few, many string) string {
	if i%10 == 1 && i%100 != 11 {
		return one
	}
	if i%10 >= 2 && i%10 <= 4 && (i%100 < 10 || i%100 >= 20) {
		return few
	}
	return many
}

// mux - описание маршрутов, отдельно от Start, чтобы их можно было проверить
// без поднятия реального порта.
func (s *Server) mux() *http.ServeMux {
	mux := http.NewServeMux()

	// Pages
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/map", s.handleMap)

	// API
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/sync", s.handleSync)
	mux.HandleFunc("/api/map", s.handleMapData)

	// MCP (Model Context Protocol) over streamable HTTP, if configured.
	if s.mcpHandler != nil {
		mux.Handle("/mcp", s.mcpHandler)
		mux.Handle("/mcp/", s.mcpHandler)
	}

	return mux
}

// Start - starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	server := &http.Server{
		Addr:    s.addr,
		Handler: s.mux(),
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down server: %v", err)
		}
	}()

	log.Printf("Server starting on %s", s.addr)
	return server.ListenAndServe()
}
