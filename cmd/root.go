package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ytbs/indexer"

	"github.com/spf13/cobra"
)

// Build-time metadata. Overridden by goreleaser via -ldflags
// (-X ytbs/cmd.version=... -X ytbs/cmd.commit=... -X ytbs/cmd.date=...).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var (
	manticoreURL string
	trackerToken string
	trackerOrgID string

	ctx context.Context
	idx *indexer.Indexer
)

var rootCmd = &cobra.Command{
	Use:   "ytbs",
	Short: "Yandex Tracker Better Search",
	Long: `Search and index issues from Yandex Tracker using full-text search.

Indexes issues and comments from Yandex Tracker into Manticore Search
for fast full-text searching with rich filtering capabilities.`,

	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		trackerToken = os.Getenv("TRACKER_OAUTH_TOKEN")
		trackerOrgID = os.Getenv("TRACKER_CLOUD_ORG_ID")
		manticoreURL = os.Getenv("MANTICORE_URL")

		if manticoreURL == "" {
			manticoreURL = "http://localhost:9308"
		}

		var cancel context.CancelFunc
		ctx, cancel = signal.NotifyContext(
			context.Background(),
			syscall.SIGINT, syscall.SIGTERM,
		)
		_ = cancel

		idx = indexer.NewIndexer(manticoreURL)
		if err := idx.CreateTable(ctx); err != nil {
			return fmt.Errorf("failed to create Manticore table: %w", err)
		}

		return nil
	},
}

// RequireTrackerEnv ensures TRACKER_OAUTH_TOKEN and TRACKER_CLOUD_ORG_ID are
// set. Subcommands that talk to the Yandex Tracker API (sync, serve) call
// this from their PreRunE. Read-only subcommands (search, mcp) skip it.
func RequireTrackerEnv() error {
	if trackerToken == "" {
		return fmt.Errorf("TRACKER_OAUTH_TOKEN environment variable is required")
	}
	if trackerOrgID == "" {
		return fmt.Errorf("TRACKER_CLOUD_ORG_ID environment variable is required")
	}
	return nil
}

// Execute — main execution function
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func init() {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose logging")

	rootCmd.SetHelpTemplate(`{{.Long}}

Usage:
  {{.UseLine}}

Available Commands:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding}} {{.Short}}{{end}}{{end}}

Flags:
{{.LocalFlags.FlagUsages}}
Global Flags:
{{.InheritedFlags.FlagUsages}}
Environment Variables:
  TRACKER_OAUTH_TOKEN   OAuth token for Yandex Tracker (required for sync/serve)
  TRACKER_CLOUD_ORG_ID  Cloud Organization ID (required for sync/serve)
  MANTICORE_URL         Manticore Search URL (default: http://localhost:9308)
  ATTACHMENT_TEXT_MAX_BYTES  Max size in bytes for downloading/indexing text attachments (default: 2097152)

Use "{{.CommandPath}} [command] --help" for more information about a command.
`)
}

func GetContext() context.Context  { return ctx }
func GetIndexer() *indexer.Indexer { return idx }
func GetTrackerToken() string      { return trackerToken }
func GetTrackerOrgID() string      { return trackerOrgID }
