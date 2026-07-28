package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/SSSKrut/yandex-tracker-better-search/internal/indexer"
	"github.com/SSSKrut/yandex-tracker-better-search/internal/searchapi"
	syncer "github.com/SSSKrut/yandex-tracker-better-search/internal/sync"
)

func TestMain(m *testing.M) {
	// Handlers log on their degraded paths, which is noise in test output.
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// fakeAPI - a searchService the test drives entirely.
type fakeAPI struct {
	status        searchapi.FullStatus
	logs          []syncer.LogEntry
	filterOptions *searchapi.FilterOptions
	filterErr     error
	results       []searchapi.IndexerSearchResult
	searchErr     error
	mapData       *indexer.MapData
	mapErr        error
	triggerErr    error
	cancelErr     error

	searchCalls  []searchapi.SearchParams
	triggerCalls []string
	cancelCalls  int
}

func (f *fakeAPI) Status() searchapi.FullStatus { return f.status }
func (f *fakeAPI) Logs(int) []syncer.LogEntry   { return f.logs }
func (f *fakeAPI) TriggerSync(mode string) error {
	f.triggerCalls = append(f.triggerCalls, mode)
	return f.triggerErr
}
func (f *fakeAPI) CancelSync() error { f.cancelCalls++; return f.cancelErr }

func (f *fakeAPI) GetFilterOptions(context.Context) (*searchapi.FilterOptions, error) {
	if f.filterErr != nil {
		return nil, f.filterErr
	}
	if f.filterOptions == nil {
		return &searchapi.FilterOptions{}, nil
	}
	return f.filterOptions, nil
}

func (f *fakeAPI) SearchRich(_ context.Context, p searchapi.SearchParams) ([]searchapi.IndexerSearchResult, error) {
	f.searchCalls = append(f.searchCalls, p)
	return f.results, f.searchErr
}

func (f *fakeAPI) Map(_ context.Context, _ bool) (*indexer.MapData, error) {
	if f.mapErr != nil {
		return nil, f.mapErr
	}
	if f.mapData == nil {
		return &indexer.MapData{}, nil
	}
	return f.mapData, nil
}

func newTestServer(t *testing.T, api *fakeAPI) *Server {
	t.Helper()

	srv, err := NewServer(":0", api, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func do(t *testing.T, h http.HandlerFunc, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestNewServer_ParsesTemplates(t *testing.T) {
	srv := newTestServer(t, &fakeAPI{})

	for _, name := range []string{"index.html", "logs.html", "map.html", "results.html", "status.html"} {
		if srv.templates.Lookup(name) == nil {
			t.Errorf("template %q not parsed", name)
		}
	}
}

func TestHandleIndex_NotFoundOnOtherPaths(t *testing.T) {
	srv := newTestServer(t, &fakeAPI{})

	rec := do(t, srv.handleIndex, http.MethodGet, "/no-such-page")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a non-root path, got %d", rec.Code)
	}
}

func TestHandleIndex_RendersWithFilters(t *testing.T) {
	api := &fakeAPI{filterOptions: &searchapi.FilterOptions{Queues: []string{"NOVA"}}}
	srv := newTestServer(t, api)

	rec := do(t, srv.handleIndex, http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "NOVA") {
		t.Error("expected the queue filter to be rendered")
	}
}

func TestHandleIndex_SurvivesFilterOptionsError(t *testing.T) {
	// The page must survive filters failing to load.
	api := &fakeAPI{filterErr: fmt.Errorf("manticore is down")}
	srv := newTestServer(t, api)

	rec := do(t, srv.handleIndex, http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite the filter error, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected a rendered page")
	}
}

func TestHandleSearch_EmptyQueryDoesNotSearch(t *testing.T) {
	api := &fakeAPI{}
	srv := newTestServer(t, api)

	rec := do(t, srv.handleSearch, http.MethodGet, "/api/search?q=")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(api.searchCalls) != 0 {
		t.Fatalf("empty query must not hit the search service, got %d calls", len(api.searchCalls))
	}
}

func TestHandleSearch_FiltersAloneTriggerSearch(t *testing.T) {
	// An empty query with a filter set is still a search.
	api := &fakeAPI{}
	srv := newTestServer(t, api)

	do(t, srv.handleSearch, http.MethodGet, "/api/search?q=&queue=NOVA")
	if len(api.searchCalls) != 1 {
		t.Fatalf("expected one search call, got %d", len(api.searchCalls))
	}
	if api.searchCalls[0].Queue != "NOVA" {
		t.Errorf("queue filter not passed through: %+v", api.searchCalls[0])
	}
}

func TestHandleSearch_PassesAllFilters(t *testing.T) {
	api := &fakeAPI{}
	srv := newTestServer(t, api)

	do(t, srv.handleSearch, http.MethodGet,
		"/api/search?q=bug&queue=NOVA&status=Open&priority=high&author=alex&assignee=kim")

	if len(api.searchCalls) != 1 {
		t.Fatalf("expected one search call, got %d", len(api.searchCalls))
	}

	got := api.searchCalls[0]
	want := searchapi.SearchParams{
		Query: "bug", Queue: "NOVA", Status: "Open",
		Priority: "high", Author: "alex", Assignee: "kim", Limit: 50,
	}
	if got != want {
		t.Errorf("params mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestHandleSearch_RendersResults(t *testing.T) {
	api := &fakeAPI{results: []searchapi.IndexerSearchResult{
		{Kind: "issue", Key: "NOVA-42", Summary: "Кнопка не работает", URL: "https://tracker.yandex.ru/NOVA-42"},
	}}
	srv := newTestServer(t, api)

	rec := do(t, srv.handleSearch, http.MethodGet, "/api/search?q=кнопка")
	if !strings.Contains(rec.Body.String(), "NOVA-42") {
		t.Error("expected the result key in the response body")
	}
}

func TestHandleSearch_RendersError(t *testing.T) {
	api := &fakeAPI{searchErr: fmt.Errorf("sql error: P01 syntax error")}
	srv := newTestServer(t, api)

	rec := do(t, srv.handleSearch, http.MethodGet, "/api/search?q=https://")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with an in-page error, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "P01") {
		t.Error("expected the search error to reach the page")
	}
}

func TestHandleStatus_Renders(t *testing.T) {
	srv := newTestServer(t, &fakeAPI{})

	rec := do(t, srv.handleStatus, http.MethodGet, "/api/status")
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("expected a rendered status, got %d / %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestHandleSync_Triggers(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		api         *fakeAPI
		wantTrigger string
	}{
		{"post ok", http.MethodPost, &fakeAPI{}, "sync-started"},
		{"post error", http.MethodPost, &fakeAPI{triggerErr: fmt.Errorf("already running")}, "sync-error"},
		{"delete ok", http.MethodDelete, &fakeAPI{}, "sync-cancelled"},
		{"delete error", http.MethodDelete, &fakeAPI{cancelErr: fmt.Errorf("sync not in progress")}, "sync-error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t, tc.api)

			rec := do(t, srv.handleSync, tc.method, "/api/sync")
			if got := rec.Header().Get("HX-Trigger"); got != tc.wantTrigger {
				t.Errorf("HX-Trigger = %q, want %q", got, tc.wantTrigger)
			}
			if rec.Body.Len() == 0 {
				t.Error("expected the status partial to be rendered")
			}
		})
	}
}

func TestHandleSync_GetDoesNothing(t *testing.T) {
	api := &fakeAPI{}
	srv := newTestServer(t, api)

	rec := do(t, srv.handleSync, http.MethodGet, "/api/sync")
	if got := rec.Header().Get("HX-Trigger"); got != "" {
		t.Errorf("GET must not signal anything, got %q", got)
	}
	if len(api.triggerCalls) != 0 || api.cancelCalls != 0 {
		t.Error("GET must not touch the sync manager")
	}
}

func TestHandleMapData_JSON(t *testing.T) {
	api := &fakeAPI{mapData: &indexer.MapData{Points: []indexer.MapPoint{{Key: "NOVA-42"}}}}
	srv := newTestServer(t, api)

	rec := do(t, srv.handleMapData, http.MethodGet, "/api/map")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var payload indexer.MapData
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(payload.Points) != 1 || payload.Points[0].Key != "NOVA-42" {
		t.Errorf("unexpected payload: %+v", payload)
	}
}

func TestHandleMapData_Error(t *testing.T) {
	api := &fakeAPI{mapErr: fmt.Errorf("not enough documents")}
	srv := newTestServer(t, api)

	rec := do(t, srv.handleMapData, http.MethodGet, "/api/map")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestStart_RoutesAndMCPMount(t *testing.T) {
	// The routes and the MCP mount must match what the UI expects.
	api := &fakeAPI{}
	mcpHit := false
	mcp := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mcpHit = true
		w.WriteHeader(http.StatusTeapot)
	})

	srv, err := NewServer(":0", api, mcp)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ts := httptest.NewServer(srv.mux())
	defer ts.Close()

	for _, path := range []string{"/", "/logs", "/map", "/api/search", "/api/status", "/api/map"} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
		}
	}

	resp, err := ts.Client().Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	resp.Body.Close()
	if !mcpHit {
		t.Error("MCP handler was not mounted at /mcp")
	}
}

func TestStart_NoMCPHandler(t *testing.T) {
	srv := newTestServer(t, &fakeAPI{})

	ts := httptest.NewServer(srv.mux())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()

	// With no MCP handler, /mcp falls into the catch-all "/" and 404s.
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /mcp = %d, want 404 when MCP is disabled", resp.StatusCode)
	}
}

func TestHandleSearch_HighlightIsEscapedEndToEnd(t *testing.T) {
	// Through the real template: Manticore returns markers mixed with issue
	// text, and only our <b> may come out.
	api := &fakeAPI{results: []searchapi.IndexerSearchResult{{
		Kind: "issue", Key: "NOVA-1", Summary: "тест",
		Highlight: indexer.HighlightOpen + "кнопка" + indexer.HighlightClose +
			` <img src=x onerror=alert(1)><script>alert(2)</script>`,
	}}}
	srv := newTestServer(t, api)

	body := do(t, srv.handleSearch, http.MethodGet, "/api/search?q=кнопка").Body.String()

	const want = `<div class="result-highlight"><b>кнопка</b> ` +
		`&lt;img src=x onerror=alert(1)&gt;&lt;script&gt;alert(2)&lt;/script&gt;</div>`
	if !strings.Contains(body, want) {
		t.Errorf("highlight block rendered unsafely.\nwant to contain: %s", want)
	}
	// No tag from the payload: <b> closes before the user text begins.
	for _, bad := range []string{"<script", "<img"} {
		if strings.Contains(body, bad) {
			t.Errorf("payload leaked a tag into the page: %q", bad)
		}
	}
}
