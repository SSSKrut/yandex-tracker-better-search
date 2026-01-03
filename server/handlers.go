package server

import (
	"log"
	"net/http"

	"ytbs/indexer"
)

// handleIndex - main page
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Load filter options
	filterOptions, err := s.indexer.GetFilterOptions(r.Context())
	if err != nil {
		log.Printf("Error loading filter options: %v", err)
		filterOptions = &indexer.FilterOptions{}
	}

	data := struct {
		Status  any
		Filters *indexer.FilterOptions
	}{
		Status:  s.syncManager.GetStatus(),
		Filters: filterOptions,
	}

	s.templates.ExecuteTemplate(w, "index.html", data)
}

// handleLogs - logs page
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Logs   any
		Status any
	}{
		Logs:   s.syncManager.GetLogs(100),
		Status: s.syncManager.GetStatus(),
	}

	s.templates.ExecuteTemplate(w, "logs.html", data)
}

// handleSearch - search API (htmx)
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	// Get filter parameters
	filters := indexer.SearchFilters{
		Queue:    r.URL.Query().Get("queue"),
		Status:   r.URL.Query().Get("status"),
		Priority: r.URL.Query().Get("priority"),
		Author:   r.URL.Query().Get("author"),
		Assignee: r.URL.Query().Get("assignee"),
	}

	// Unified data structure for template
	data := struct {
		Query   string
		Results any
		Count   int
		Error   string
		Filters indexer.SearchFilters
	}{
		Query:   query,
		Filters: filters,
	}

	// Check if we have any search criteria
	hasFilters := filters.Queue != "" || filters.Status != "" || filters.Priority != "" ||
		filters.Author != "" || filters.Assignee != ""

	if query == "" && !hasFilters {
		s.templates.ExecuteTemplate(w, "results.html", data)
		return
	}

	results, err := s.indexer.SearchWithFilters(r.Context(), query, filters, 50)
	if err != nil {
		data.Error = err.Error()
		s.templates.ExecuteTemplate(w, "results.html", data)
		log.Print("Search error: ", err)
		return
	}

	data.Results = results
	data.Count = len(results)
	log.Printf("Search query: %q, filters: %+v, results: %d", query, filters, len(results))

	if err := s.templates.ExecuteTemplate(w, "results.html", data); err != nil {
		log.Printf("Template error: %v", err)
	}
}

// handleStatus - status API (htmx)
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.templates.ExecuteTemplate(w, "status.html", s.syncManager.GetStatus())
}

// handleSync - syncronization control API (htmx)
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		err := s.syncManager.TriggerSync()
		if err != nil {
			w.Header().Set("HX-Trigger", "sync-error")
		} else {
			w.Header().Set("HX-Trigger", "sync-started")
		}
	case http.MethodDelete:
		s.syncManager.CancelSync()
		w.Header().Set("HX-Trigger", "sync-cancelled")
	}

	s.templates.ExecuteTemplate(w, "status.html", s.syncManager.GetStatus())
}
