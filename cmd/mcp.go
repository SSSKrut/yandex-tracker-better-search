package cmd

import (
	"log"
	"os"

	ytbsmcp "github.com/SSSKrut/yandex-tracker-better-search/mcp"
	"github.com/SSSKrut/yandex-tracker-better-search/searchapi"
	"github.com/SSSKrut/yandex-tracker-better-search/sync"
	"github.com/SSSKrut/yandex-tracker-better-search/tracker"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run an MCP (Model Context Protocol) server over stdio",
	Long: `Expose ytbs as an MCP server for AI agents.

The server speaks MCP over stdin/stdout and surfaces six tools (search_tasks,
get_task, get_nearest_neighbors, get_map_overview, trigger_sync, get_status)
plus a tracker://issue/{key} resource template. It only reads from Manticore
and only requires MANTICORE_URL — TRACKER_OAUTH_TOKEN and TRACKER_CLOUD_ORG_ID
become required only if a client invokes trigger_sync.

Configure your MCP client (e.g. Claude Code) to launch:
  command: /path/to/ytbs
  args:    ["mcp"]
  env:     { "MANTICORE_URL": "http://localhost:9308" }`,

	RunE: func(cmd *cobra.Command, args []string) error {
		// Build a sync.Manager so MCP's trigger_sync tool can drive a sync if
		// the client asks. The manager is constructed lazily-friendly: if
		// TRACKER_* are missing, the underlying tracker.Client will fail at
		// fetch time, not at construction. Read-only MCP usage never hits it.
		client := tracker.NewClientWithAuth(GetTrackerToken(), GetTrackerAuthScheme(), GetTrackerOrgID())
		mgr := sync.NewManager(client, GetIndexer(), nil, 5, 0, 0)

		api := searchapi.NewService(GetIndexer(), mgr)
		srv := ytbsmcp.NewServer(api)

		log.SetOutput(os.Stderr) // never write to stdout — it's the MCP transport
		log.Println("ytbs MCP server starting on stdio")
		if err := srv.Run(GetContext(), &sdk.StdioTransport{}); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
