package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ytbs/indexer"
	"ytbs/server"
	"ytbs/sync"
	"ytbs/tracker"
)

var (
	manticoreURL string = os.Getenv("MANTICORE_URL")
	trackerToken string = os.Getenv("TRACKER_OAUTH_TOKEN")
	trackerOrgID string = os.Getenv("TRACKER_CLOUD_ORG_ID")

	helpText = `Yandex Tracker Better Search

Usage:
  -serve              Run web server with UI and periodic sync
  -sync               Run one-time sync from Tracker
  -search TEXT        Search for issues (CLI mode)
  -h, -help           Show this message

Server options:
  -addr :8080         HTTP server address
  -interval 15m       Sync interval (e.g. 10m, 1h)

Environment variables:
  TRACKER_OAUTH_TOKEN   - OAuth token for Yandex Tracker
  TRACKER_CLOUD_ORG_ID  - Cloud Organization ID
  MANTICORE_URL         - Manticore Search URL (default: http://localhost:9308)`
)

func main() {
	serveFlag := flag.Bool("serve", false, "Run web server with periodic sync")
	syncFlag := flag.Bool("sync", false, "Run one-time sync from Tracker")
	searchFlag := flag.String("search", "", "Search query (CLI mode)")
	addrFlag := flag.String("addr", ":8080", "HTTP server address")
	intervalFlag := flag.Duration("interval", 15*time.Minute, "Sync interval")
	helpFlag := flag.Bool("h", false, "Show help")
	helpFlagLong := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *helpFlag || *helpFlagLong {
		fmt.Println(helpText)
		return
	}

	if trackerToken == "" {
		log.Fatal("TRACKER_OAUTH_TOKEN is required")
	}
	if trackerOrgID == "" {
		log.Fatal("TRACKER_CLOUD_ORG_ID is required")
	}
	if manticoreURL == "" {
		manticoreURL = "http://localhost:9308" // default
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	idx := indexer.NewIndexer(manticoreURL)

	if err := idx.CreateTable(ctx); err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// Web server mode
	if *serveFlag {
		runServer(ctx, idx, *addrFlag, *intervalFlag)
		return
	}

	// One-time sync mode
	if *syncFlag {
		runSync(ctx, idx)
		return
	}

	// CLI search mode
	if *searchFlag != "" {
		runSearch(ctx, idx, *searchFlag)
		return
	}

	fmt.Println(helpText)
}

func runServer(ctx context.Context, idx *indexer.Indexer, addr string, interval time.Duration) {
	client := tracker.NewClient(trackerToken, trackerOrgID)

	syncMgr := sync.NewManager(client, idx, nil, 5, interval)

	go syncMgr.Start(ctx)

	srv, err := server.NewServer(addr, idx, syncMgr)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	log.Printf("Starting server on %s (sync every %s)", addr, interval)
	if err := srv.Start(ctx); err != nil {
		log.Printf("Server stopped: %v", err)
	}
}

func runSync(ctx context.Context, idx *indexer.Indexer) {
	client := tracker.NewClient(trackerToken, trackerOrgID)

	// Options: specify queues to sync, or nil/empty for all accessible
	// queues := []string{"MYQUEUE", "ANOTHER"}
	var queues []string

	log.Println("Starting initial sync from Yandex Tracker...")

	issues, result, err := client.InitialSync(ctx, queues, 5)
	if err != nil {
		log.Fatalf("Initial sync failed: %v", err)
	}

	log.Printf("Fetched from Tracker:")
	log.Printf("  - Issues: %d", result.TotalIssues)
	log.Printf("  - Comments: %d", result.TotalComments)
	log.Printf("  - Errors: %d", len(result.Errors))

	if err := idx.IndexIssues(ctx, issues); err != nil {
		log.Fatalf("Indexing failed: %v", err)
	}

	log.Println("Sync completed successfully!")
}

func runSearch(ctx context.Context, idx *indexer.Indexer, query string) {
	log.Printf("Searching for: %s", query)

	results, err := idx.Search(ctx, query, 20)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		log.Println("No results found")
		return
	}

	log.Printf("Found %d results:", len(results))
	for _, r := range results {
		log.Printf("  [%s] %s", r.Key, r.Summary)
		log.Printf("    Status: %s | Assignee: %s", r.StatusName, r.AssigneeName)
		log.Printf("    URL: %s", r.URL)
		if r.Highlight != "" {
			log.Printf("    Match: %s", r.Highlight)
		}
		log.Println()
	}
}
