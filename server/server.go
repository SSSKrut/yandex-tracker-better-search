package server

import (
	"context"
	"embed"
	"html/template"
	"log"
	"net/http"
	"time"

	"ytbs/searchapi"
)

//go:embed templates/*
var templatesFS embed.FS

// Server - HTTP server
type Server struct {
	api       *searchapi.Service
	templates *template.Template
	addr      string
}

// NewServer - creates a new Server instance
func NewServer(addr string, api *searchapi.Service) (*Server, error) {

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
	}).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Server{
		api:       api,
		templates: tmpl,
		addr:      addr,
	}, nil
}

func formatDuration(n float64, one, few, many string) string {
	i := int(n)
	if i%10 == 1 && i%100 != 11 {
		return string(rune('0'+i%10)) + " " + one
	}
	if i%10 >= 2 && i%10 <= 4 && (i%100 < 10 || i%100 >= 20) {
		return string(rune('0'+i%10)) + " " + few
	}
	return string(rune('0'+i%10)) + " " + many
}

// Start - starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
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

	server := &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Server starting on %s", s.addr)
	return server.ListenAndServe()
}
