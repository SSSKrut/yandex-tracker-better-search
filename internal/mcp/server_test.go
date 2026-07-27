package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/SSSKrut/yandex-tracker-better-search/internal/indexer"
	"github.com/SSSKrut/yandex-tracker-better-search/internal/searchapi"
	syncer "github.com/SSSKrut/yandex-tracker-better-search/internal/sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestServer_RegistersExpectedTools is a smoke test: it spins up the MCP
// server in-memory and asserts the published tool list. It does NOT exercise
// tool handlers (those would require a live Manticore).
func TestServer_RegistersExpectedTools(t *testing.T) {
	idx := indexer.NewIndexer("http://127.0.0.1:65535") // unreachable; we never call it
	mgr := syncer.NewManager(nil, idx, nil, 1, 0, 0)
	api := searchapi.NewService(idx, mgr)

	srv := NewServer(api)

	ctx := context.Background()
	t1, t2 := sdk.NewInMemoryTransports()

	if _, err := srv.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	got, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	names := make([]string, 0, len(got.Tools))
	for _, tool := range got.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)

	want := []string{
		"get_map_overview",
		"get_nearest_neighbors",
		"get_status",
		"get_task",
		"search_tasks",
		"trigger_sync",
	}

	if len(names) != len(want) {
		t.Fatalf("expected %d tools, got %d: %v", len(want), len(names), names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("tool[%d] = %q, want %q (full list: %v)", i, names[i], n, names)
		}
	}

	// trigger_sync must NOT be advertised as read-only.
	// search_tasks/get_task/get_nearest_neighbors/get_map_overview/get_status MUST be.
	wantReadOnly := map[string]bool{
		"search_tasks":          true,
		"get_task":              true,
		"get_nearest_neighbors": true,
		"get_map_overview":      true,
		"get_status":            true,
		"trigger_sync":          false,
	}
	for _, tool := range got.Tools {
		want, ok := wantReadOnly[tool.Name]
		if !ok {
			continue
		}
		gotReadOnly := tool.Annotations != nil && tool.Annotations.ReadOnlyHint
		if gotReadOnly != want {
			t.Fatalf("tool %s ReadOnlyHint = %v, want %v", tool.Name, gotReadOnly, want)
		}
	}
}

// TestBearerAuth covers the three branches: open endpoint, valid token, bad token.
func TestBearerAuth(t *testing.T) {
	idx := indexer.NewIndexer("http://127.0.0.1:65535")
	mgr := syncer.NewManager(nil, idx, nil, 1, 0, 0)
	api := searchapi.NewService(idx, mgr)

	cases := []struct {
		name     string
		token    string
		header   string
		wantCode int
	}{
		{"open endpoint accepts missing header", "", "", http.StatusOK},
		{"valid bearer token", "secret", "Bearer secret", http.StatusOK},
		{"missing header is rejected", "secret", "", http.StatusUnauthorized},
		{"wrong token is rejected", "secret", "Bearer wrong", http.StatusUnauthorized},
		{"missing Bearer prefix is rejected", "secret", "secret", http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHTTPHandler(api, tc.token)
			// A bare GET on the root MCP path lands in the StreamableHTTPHandler, which
			// will return 4xx (no MCP session) — but only AFTER auth middleware has
			// passed. We only care that auth either rejects (401) or lets the request
			// through to MCP (anything else).
			req := httptest.NewRequest("GET", "/mcp", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if tc.wantCode == http.StatusUnauthorized {
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("got %d, want 401", rec.Code)
				}
				return
			}
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("auth rejected a request that should have passed: header=%q", tc.header)
			}
		})
	}
}

// TestErrorResult_MarksIsError verifies the helper used by tool handlers.
func TestErrorResult_MarksIsError(t *testing.T) {
	res := errorResult("oops")
	if !res.IsError {
		t.Fatalf("expected IsError=true")
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(res.Content))
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	if tc.Text != "oops" {
		t.Fatalf("text = %q, want oops", tc.Text)
	}
}
