package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/SSSKrut/yandex-tracker-better-search/internal/searchapi"
)

// handleIndex - main page
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	filterOptions, err := s.api.GetFilterOptions(r.Context())
	if err != nil {
		log.Printf("Error loading filter options: %v", err)
		filterOptions = &searchapi.FilterOptions{}
	}

	data := struct {
		Status  any
		Filters *searchapi.FilterOptions
	}{
		Status:  s.api.Status().Status,
		Filters: filterOptions,
	}

	s.render(w, "index.html", data)
}

// handleLogs - logs page
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Logs   any
		Status any
	}{
		Logs:   s.api.Logs(100),
		Status: s.api.Status().Status,
	}

	s.render(w, "logs.html", data)
}

// handleMap - map visualization page
func (s *Server) handleMap(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Status any
	}{
		Status: s.api.Status().Status,
	}

	s.render(w, "map.html", data)
}

// handleMapData - map data API
func (s *Server) handleMapData(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"

	data, err := s.api.Map(r.Context(), refresh)
	if err != nil {
		log.Printf("Map build error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, data)
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		log.Printf("JSON encode error: %v", err)
	}
}

// handleSearch - search API (htmx)
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	params := searchapi.SearchParams{
		Query:    query,
		Queue:    r.URL.Query().Get("queue"),
		Status:   r.URL.Query().Get("status"),
		Priority: r.URL.Query().Get("priority"),
		Author:   r.URL.Query().Get("author"),
		Assignee: r.URL.Query().Get("assignee"),
		Limit:    50,
	}

	data := struct {
		Query   string
		Results any
		Count   int
		Error   string
		Filters searchapi.SearchParams
	}{
		Query:   query,
		Filters: params,
	}

	hasFilters := params.Queue != "" || params.Status != "" || params.Priority != "" ||
		params.Author != "" || params.Assignee != ""

	if query == "" && !hasFilters {
		s.render(w, "results.html", data)
		return
	}

	results, err := s.api.SearchRich(r.Context(), params)
	if err != nil {
		data.Error = err.Error()
		s.render(w, "results.html", data)
		log.Print("Search error: ", err)
		return
	}

	data.Results = results
	data.Count = len(results)
	log.Printf("Search query: %q, filters: %+v, results: %d", query, params, len(results))

	s.render(w, "results.html", data)
}

// handleStatus - status API (htmx)
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.render(w, "status.html", s.api.Status().Status)
}

// handleSync - synchronization control API (htmx)
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if err := s.api.TriggerSync(""); err != nil {
			w.Header().Set("HX-Trigger", "sync-error")
		} else {
			w.Header().Set("HX-Trigger", "sync-started")
		}
	case http.MethodDelete:
		if err := s.api.CancelSync(); err != nil {
			log.Printf("Error cancelling sync: %v", err)
			w.Header().Set("HX-Trigger", "sync-error")
		} else {
			w.Header().Set("HX-Trigger", "sync-cancelled")
		}
	}

	s.render(w, "status.html", s.api.Status().Status)
}

// render - executes a template. The response is already partly written by
// then, so a failure can only be logged.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("Error rendering %s: %v", name, err)
	}
}
