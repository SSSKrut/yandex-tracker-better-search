package cmd

import (
	"log"
	"os"
	"strings"
	"time"

	ytbsmcp "ytbs/mcp"
	"ytbs/searchapi"
	"ytbs/server"
	"ytbs/sync"
	"ytbs/tracker"

	"github.com/spf13/cobra"
)

var (
	serveAddr         string
	serveInterval     time.Duration
	serveFullInterval time.Duration
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run web server with periodic sync",
	Long: `Start HTTP server with web UI for searching issues.
    
Automatically syncs with Yandex Tracker at specified intervals.
Provides web interface for:
  - Full-text search with filters
  - Manual sync triggering
  - Sync status and logs`,

	PreRunE: func(cmd *cobra.Command, args []string) error { return RequireTrackerEnv() },

	RunE: func(cmd *cobra.Command, args []string) error {
		client := tracker.NewClientWithAuth(GetTrackerToken(), GetTrackerAuthScheme(), GetTrackerOrgID())

		syncMgr := sync.NewManager(
			client,
			GetIndexer(),
			nil, // queues (nil = all)
			5,   // workers
			serveInterval,
			serveFullInterval,
		)

		// Background sync at startup
		go syncMgr.Start(GetContext())

		api := searchapi.NewService(GetIndexer(), syncMgr)

		mcpToken := os.Getenv("MCP_AUTH_TOKEN")
		mcpHandler := ytbsmcp.NewHTTPHandler(api, mcpToken)
		if mcpToken == "" && !isLoopback(serveAddr) {
			log.Printf("warning: MCP endpoint /mcp is unauthenticated; set MCP_AUTH_TOKEN to require Bearer auth")
		}

		srv, err := server.NewServer(serveAddr, api, mcpHandler)
		if err != nil {
			return err
		}

		log.Printf("Starting server on %s (incremental: %s, full: %s)", serveAddr, serveInterval, serveFullInterval)
		return srv.Start(GetContext())
	},
}

func init() {
	serveCmd.Flags().StringVarP(&serveAddr, "addr", "a", ":8080", "HTTP server address")
	serveCmd.Flags().DurationVarP(&serveInterval, "interval", "i", 15*time.Minute, "Incremental sync interval (e.g. 10m, 1h, 30s)")
	serveCmd.Flags().DurationVar(&serveFullInterval, "full-interval", 24*time.Hour, "Full sync interval (e.g. 24h)")

	rootCmd.AddCommand(serveCmd)
}

func isLoopback(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	switch host {
	case "", "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}
