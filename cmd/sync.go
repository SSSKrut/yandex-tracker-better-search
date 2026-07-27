package cmd

import (
	"log"

	"github.com/SSSKrut/yandex-tracker-better-search/tracker"

	"github.com/spf13/cobra"
)

var (
	syncQueues  []string
	syncWorkers int
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Run one-time sync from Tracker",
	Long: `Fetch all issues and comments from Yandex Tracker and index them.

Performs a complete sync of:
  - All issues from specified queues (or all accessible queues)
  - All comments for each issue
  - Indexes everything into Manticore Search`,

	PreRunE: func(cmd *cobra.Command, args []string) error { return RequireTrackerEnv() },

	RunE: func(cmd *cobra.Command, args []string) error {
		client := tracker.NewClientWithAuth(GetTrackerToken(), GetTrackerAuthScheme(), GetTrackerOrgID())

		log.Println("Starting sync from Yandex Tracker...")
		if len(syncQueues) > 0 {
			log.Printf("Syncing queues: %v", syncQueues)
		} else {
			log.Println("Syncing all accessible queues")
		}

		issues, files, result, err := client.InitialSync(GetContext(), syncQueues, syncWorkers)
		if err != nil {
			return err
		}

		log.Printf("Fetched from Tracker:")
		log.Printf("  Issues:   %d", result.TotalIssues)
		log.Printf("  Comments: %d", result.TotalComments)
		log.Printf("  Files:    %d (text: %d)", result.TotalFiles, result.TextFiles)
		log.Printf("  Errors:   %d", len(result.Errors))

		if len(result.Errors) > 0 {
			log.Println("\nErrors encountered:")
			for _, e := range result.Errors {
				log.Printf("  - %s", e)
			}
		}

		log.Println("\nIndexing into Manticore...")
		if err := GetIndexer().IndexIssues(GetContext(), issues); err != nil {
			return err
		}
		if err := GetIndexer().IndexFiles(GetContext(), files); err != nil {
			return err
		}

		log.Println("Sync completed successfully!")
		return nil
	},
}

func init() {
	syncCmd.Flags().StringSliceVarP(&syncQueues, "queues", "q", nil, "Specific queues to sync (comma-separated)")
	syncCmd.Flags().IntVarP(&syncWorkers, "workers", "w", 5, "Number of concurrent workers for fetching")

	rootCmd.AddCommand(syncCmd)
}
