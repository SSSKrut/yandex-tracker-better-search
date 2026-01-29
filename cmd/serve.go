package cmd

import (
	"log"
	"time"

	"ytbs/server"
	"ytbs/sync"
	"ytbs/tracker"

	"github.com/spf13/cobra"
)

var (
	serveAddr     string
	serveInterval time.Duration
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

	RunE: func(cmd *cobra.Command, args []string) error {
		client := tracker.NewClient(GetTrackerToken(), GetTrackerOrgID())

		syncMgr := sync.NewManager(
			client,
			GetIndexer(),
			nil, // queues (nil = all)
			5,   // workers
			serveInterval,
		)

		// Background sync at startup
		go syncMgr.Start(GetContext())

		srv, err := server.NewServer(serveAddr, GetIndexer(), syncMgr)
		if err != nil {
			return err
		}

		log.Printf("Starting server on %s (sync every %s)", serveAddr, serveInterval)
		return srv.Start(GetContext())
	},
}

func init() {
	serveCmd.Flags().StringVarP(&serveAddr, "addr", "a", ":8080", "HTTP server address")
	serveCmd.Flags().DurationVarP(&serveInterval, "interval", "i", 15*time.Minute, "Sync interval (e.g. 10m, 1h, 30s)")

	rootCmd.AddCommand(serveCmd)
}
