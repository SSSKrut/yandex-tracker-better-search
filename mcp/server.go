// Package mcp wires ytbs's *searchapi.Service to a Model Context Protocol
// server. It is consumed by the `ytbs mcp` subcommand and exposes a small set
// of read-mostly tools plus a tracker://issue/{key} resource template.
package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/SSSKrut/yandex-tracker-better-search/searchapi"
	syncer "github.com/SSSKrut/yandex-tracker-better-search/sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "ytbs"
	serverVersion = "1.0.0"
)

// NewServer constructs an MCP server with all ytbs tools and resources
// pre-registered. The server is not started — callers run it via
// server.Run(ctx, transport).
func NewServer(api *searchapi.Service) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, &sdk.ServerOptions{
		Instructions: "Search Yandex Tracker issues indexed by ytbs. Tools return compact records; use get_task to fetch full text for a specific issue.",
	})

	registerSearchTasks(srv, api)
	registerGetTask(srv, api)
	registerNeighbors(srv, api)
	registerMapOverview(srv, api)
	registerTriggerSync(srv, api)
	registerStatus(srv, api)
	registerIssueResource(srv, api)

	return srv
}

// boolPtr is a tiny helper for setting *bool annotations.
func boolPtr(b bool) *bool { return &b }

// NewHTTPHandler returns an http.Handler that exposes the MCP server over the
// streamable-HTTP transport. Mount it on a route like /mcp on your existing
// HTTP server. If authToken is non-empty, requests must carry
// `Authorization: Bearer <authToken>`; otherwise the endpoint is open (intended
// for loopback / dev only).
func NewHTTPHandler(api *searchapi.Service, authToken string) http.Handler {
	srv := NewServer(api)
	h := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return srv },
		nil,
	)
	if authToken == "" {
		return h
	}
	return bearerAuth(authToken, h)
}

func bearerAuth(token string, next http.Handler) http.Handler {
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ytbs"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- search_tasks ----------

type searchTasksArgs struct {
	Query    string `json:"query,omitempty" jsonschema:"full-text query; supports prefix and infix search via Manticore"`
	Assignee string `json:"assignee,omitempty" jsonschema:"filter by assignee display name (exact match)"`
	Status   string `json:"status,omitempty" jsonschema:"filter by status display name (exact match)"`
	Queue    string `json:"queue,omitempty" jsonschema:"filter by queue key (e.g. PRJ)"`
	Priority string `json:"priority,omitempty" jsonschema:"filter by priority key"`
	Author   string `json:"author,omitempty" jsonschema:"filter by author display name (exact match)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max results to return (default 20, max 100)"`
}

type searchTasksResult struct {
	Hits  []searchapi.SearchHit `json:"hits"`
	Total int                   `json:"total"`
}

func registerSearchTasks(srv *sdk.Server, api *searchapi.Service) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "search_tasks",
		Title:       "Search tasks",
		Description: "Full-text search across indexed Yandex Tracker issues and attachments. Returns a compact list — call get_task to read full description and comments. Supports filters: assignee, status, queue, priority, author. Use this when you have keywords or know specific filter values.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *sdk.CallToolRequest, args searchTasksArgs) (*sdk.CallToolResult, any, error) {
		hits, err := api.Search(ctx, searchapi.SearchParams{
			Query:    args.Query,
			Assignee: args.Assignee,
			Status:   args.Status,
			Queue:    args.Queue,
			Priority: args.Priority,
			Author:   args.Author,
			Limit:    args.Limit,
		})
		if err != nil {
			return errorResult(fmt.Sprintf("search backend error: %v", err)), nil, nil
		}

		out := searchTasksResult{Hits: hits, Total: len(hits)}
		text := formatHits(hits)
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: text}},
			StructuredContent: out,
		}, out, nil
	})
}

func formatHits(hits []searchapi.SearchHit) string {
	if len(hits) == 0 {
		return "No matches."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d result(s):\n\n", len(hits))
	for _, h := range hits {
		switch h.Kind {
		case "file":
			fmt.Fprintf(&b, "- 📎 %s — %s (issue %s, %s)\n", h.FileName, h.MimeType, h.Key, h.URL)
		default:
			status := h.StatusName
			if status == "" {
				status = "—"
			}
			assignee := h.AssigneeName
			if assignee == "" {
				assignee = "unassigned"
			}
			fmt.Fprintf(&b, "- %s [%s] %s — %s (%s)\n", h.Key, status, h.Summary, assignee, h.URL)
		}
	}
	return b.String()
}

// ---------- get_task ----------

type getTaskArgs struct {
	Key  string `json:"key" jsonschema:"tracker issue key, e.g. PRJ-123"`
	Full bool   `json:"full,omitempty" jsonschema:"if true, return untruncated description and comments (otherwise capped at 2000 characters each)"`
}

func registerGetTask(srv *sdk.Server, api *searchapi.Service) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "get_task",
		Title:       "Get task details",
		Description: "Return the full description, comments and attachment list for a single tracker issue identified by its key (e.g. PRJ-42). Description and comments are truncated to 2000 characters by default — pass full=true if the truncated portion looks important.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *sdk.CallToolRequest, args getTaskArgs) (*sdk.CallToolResult, any, error) {
		if args.Key == "" {
			return errorResult("key is required"), nil, nil
		}
		detail, err := api.GetIssue(ctx, args.Key, args.Full)
		if err != nil {
			if errors.Is(err, searchapi.ErrIssueNotFound) {
				return errorResult(fmt.Sprintf("Issue %s not found", args.Key)), nil, nil
			}
			return errorResult(fmt.Sprintf("search backend error: %v", err)), nil, nil
		}

		text := formatIssueDetail(detail)
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: text}},
			StructuredContent: detail,
		}, detail, nil
	})
}

func formatIssueDetail(d *searchapi.IssueDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n", d.Key, d.Summary)
	fmt.Fprintf(&b, "URL: %s\n", d.URL)
	if d.StatusName != "" {
		fmt.Fprintf(&b, "Status: %s\n", d.StatusName)
	}
	if d.AssigneeName != "" {
		fmt.Fprintf(&b, "Assignee: %s\n", d.AssigneeName)
	}
	if d.AuthorName != "" {
		fmt.Fprintf(&b, "Author: %s\n", d.AuthorName)
	}
	if d.Queue != "" {
		fmt.Fprintf(&b, "Queue: %s\n", d.Queue)
	}
	if d.Priority != "" {
		fmt.Fprintf(&b, "Priority: %s\n", d.Priority)
	}
	if !d.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, "Updated: %s\n", d.UpdatedAt.Format("2006-01-02 15:04"))
	}
	b.WriteString("\n## Description\n\n")
	if d.Description == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(d.Description)
		b.WriteString("\n")
	}
	if d.CommentsText != "" {
		b.WriteString("\n## Comments\n\n")
		b.WriteString(d.CommentsText)
		b.WriteString("\n")
	}
	if len(d.Attachments) > 0 {
		b.WriteString("\n## Attachments\n\n")
		for _, a := range d.Attachments {
			fmt.Fprintf(&b, "- %s", a.FileName)
			if a.MimeType != "" {
				fmt.Fprintf(&b, " (%s)", a.MimeType)
			}
			if a.Size > 0 {
				fmt.Fprintf(&b, ", %d bytes", a.Size)
			}
			fmt.Fprintf(&b, " — %s\n", a.URL)
		}
	}
	if d.Truncated {
		b.WriteString("\n_Output truncated. Call get_task again with full=true for the complete content._\n")
	}
	return b.String()
}

// ---------- get_nearest_neighbors ----------

type neighborsArgs struct {
	Key string `json:"key" jsonschema:"tracker issue key, e.g. PRJ-123"`
	K   int    `json:"k,omitempty" jsonschema:"number of neighbours to return (default 5, max 10)"`
}

func registerNeighbors(srv *sdk.Server, api *searchapi.Service) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "get_nearest_neighbors",
		Title:       "Get similar tasks",
		Description: "Return up to k tasks that are most similar to the given issue based on the LSA-derived 2D similarity map. Use this to find prior incidents or related work for an issue. Note: the issue must have made it into the map (issues older than the MAP_MAX_ISSUES window are excluded).",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *sdk.CallToolRequest, args neighborsArgs) (*sdk.CallToolResult, any, error) {
		if args.Key == "" {
			return errorResult("key is required"), nil, nil
		}
		k := args.K
		if k <= 0 {
			k = 5
		}
		if k > 10 {
			k = 10
		}
		neighbors, err := api.Neighbors(ctx, args.Key, k)
		if err != nil {
			if errors.Is(err, searchapi.ErrNotInMap) {
				return errorResult(fmt.Sprintf("Issue %s is not in the similarity map (likely older than MAP_MAX_ISSUES)", args.Key)), nil, nil
			}
			return errorResult(fmt.Sprintf("map error: %v", err)), nil, nil
		}

		text := formatNeighbors(args.Key, neighbors)
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: text}},
			StructuredContent: neighbors,
		}, neighbors, nil
	})
}

func formatNeighbors(key string, ns []searchapi.Neighbor) string {
	if len(ns) == 0 {
		return fmt.Sprintf("No neighbours found for %s.", key)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Nearest neighbours of %s (cosine similarity):\n\n", key)
	for _, n := range ns {
		fmt.Fprintf(&b, "- %.3f  %s — %s (%s)\n", n.Score, n.Key, n.Title, n.URL)
	}
	return b.String()
}

// ---------- get_map_overview ----------

func registerMapOverview(srv *sdk.Server, api *searchapi.Service) {
	type emptyArgs struct{}
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "get_map_overview",
		Title:       "Get cluster overview",
		Description: "Return a high-level overview of the indexed corpus as clusters of related issues. Each cluster includes its size, top keywords, and 3 representative issue keys. Useful when answering 'what is the team mostly working on?' or scoping unfamiliar projects.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *sdk.CallToolRequest, args emptyArgs) (*sdk.CallToolResult, any, error) {
		clusters, err := api.MapOverview(ctx)
		if err != nil {
			return errorResult(fmt.Sprintf("map error: %v", err)), nil, nil
		}
		text := formatClusters(clusters)
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: text}},
			StructuredContent: clusters,
		}, clusters, nil
	})
}

func formatClusters(clusters []searchapi.ClusterSummary) string {
	if len(clusters) == 0 {
		return "Map is empty (no issues indexed yet)."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d cluster(s):\n\n", len(clusters))
	for _, c := range clusters {
		fmt.Fprintf(&b, "- cluster %d (size %d): %s\n", c.ID, c.Size, strings.Join(c.TopKeywords, ", "))
		if len(c.CentralKeys) > 0 {
			fmt.Fprintf(&b, "    central: %s\n", strings.Join(c.CentralKeys, ", "))
		}
	}
	return b.String()
}

// ---------- trigger_sync ----------

type triggerSyncArgs struct {
	Mode string `json:"mode,omitempty" jsonschema:"sync mode: 'incremental' (default) or 'full'"`
}

func registerTriggerSync(srv *sdk.Server, api *searchapi.Service) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "trigger_sync",
		Title:       "Trigger sync from Tracker",
		Description: "Start a manual sync from Yandex Tracker. Returns immediately — the sync runs in the background. Default mode is 'incremental' (fetches issues updated since the last sync); pass mode='full' for a complete refresh. Call get_status afterwards to monitor progress.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: false, DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, req *sdk.CallToolRequest, args triggerSyncArgs) (*sdk.CallToolResult, any, error) {
		mode := args.Mode
		if mode == "" {
			mode = syncer.ModeIncremental
		}
		if err := api.TriggerSync(mode); err != nil {
			return errorResult(fmt.Sprintf("trigger sync: %v", err)), nil, nil
		}
		text := fmt.Sprintf("Triggered %s sync. Check get_status for progress.", mode)
		out := map[string]string{"mode": mode, "status": "started"}
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: text}},
			StructuredContent: out,
		}, out, nil
	})
}

// ---------- get_status ----------

func registerStatus(srv *sdk.Server, api *searchapi.Service) {
	type emptyArgs struct{}
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "get_status",
		Title:       "Get sync status",
		Description: "Return current sync status: in-flight flag, last sync timestamps (incremental/full), counts (issues, files, comments) and the time the similarity map was last built.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *sdk.CallToolRequest, args emptyArgs) (*sdk.CallToolResult, any, error) {
		st := api.Status()
		text := formatStatus(st)
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: text}},
			StructuredContent: st,
		}, st, nil
	})
}

func formatStatus(st searchapi.FullStatus) string {
	var b strings.Builder
	if st.InProgress {
		b.WriteString("Sync: in progress\n")
	} else {
		b.WriteString("Sync: idle\n")
	}
	if st.LastSyncError != "" {
		fmt.Fprintf(&b, "Last error: %s\n", st.LastSyncError)
	}
	if !st.LastSyncAt.IsZero() {
		fmt.Fprintf(&b, "Last sync: %s\n", st.LastSyncAt.Format("2006-01-02 15:04:05"))
	}
	if !st.LastFullSyncAt.IsZero() {
		fmt.Fprintf(&b, "Last full sync: %s\n", st.LastFullSyncAt.Format("2006-01-02 15:04:05"))
	}
	if !st.LastIncrementalSyncAt.IsZero() {
		fmt.Fprintf(&b, "Last incremental sync: %s\n", st.LastIncrementalSyncAt.Format("2006-01-02 15:04:05"))
	}
	fmt.Fprintf(&b, "Issues: %d, comments: %d, files: %d (text: %d)\n",
		st.IssuesCount, st.CommentsCount, st.FilesCount, st.TextFiles)
	if !st.MapBuiltAt.IsZero() {
		fmt.Fprintf(&b, "Map built: %s\n", st.MapBuiltAt.Format("2006-01-02 15:04:05"))
	}
	return b.String()
}

// ---------- resource: tracker://issue/{key} ----------

const issueURIPrefix = "tracker://issue/"

func registerIssueResource(srv *sdk.Server, api *searchapi.Service) {
	handler := func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		uri := req.Params.URI
		if !strings.HasPrefix(uri, issueURIPrefix) {
			return nil, sdk.ResourceNotFoundError(uri)
		}
		key := strings.TrimPrefix(uri, issueURIPrefix)
		if key == "" {
			return nil, sdk.ResourceNotFoundError(uri)
		}

		detail, err := api.GetIssue(ctx, key, true)
		if err != nil {
			if errors.Is(err, searchapi.ErrIssueNotFound) {
				return nil, sdk.ResourceNotFoundError(uri)
			}
			return nil, err
		}

		return &sdk.ReadResourceResult{
			Contents: []*sdk.ResourceContents{
				{URI: uri, MIMEType: "text/markdown", Text: formatIssueDetail(detail)},
			},
		}, nil
	}

	srv.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "Tracker issue",
		Title:       "Tracker issue",
		Description: "A single Yandex Tracker issue rendered as markdown (description + comments + attachments).",
		MIMEType:    "text/markdown",
		URITemplate: "tracker://issue/{key}",
	}, handler)
}

// ---------- helpers ----------

// errorResult builds a tool response that signals an error to the client. The
// SDK marks responses as IsError so the model can recover instead of crashing.
func errorResult(msg string) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: msg}},
	}
}
