package mcp

import (
	"context"
	"sort"
	"testing"

	"ytbs/indexer"
	"ytbs/searchapi"
	syncer "ytbs/sync"

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
