package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/SSSKrut/yandex-tracker-better-search/internal/config"
	"github.com/SSSKrut/yandex-tracker-better-search/internal/indexer"
	"github.com/SSSKrut/yandex-tracker-better-search/internal/searchapi"
	syncer "github.com/SSSKrut/yandex-tracker-better-search/internal/sync"
	"github.com/SSSKrut/yandex-tracker-better-search/internal/tracker"

	"github.com/spf13/cobra"
)

// Build-time metadata. Overridden by goreleaser via -ldflags (-X main.version=...).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var (
	cfg *config.Config

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
		// The only place the environment is read; from here the values
		// travel as constructor arguments.
		loaded, err := config.Load()
		if err != nil {
			return err
		}
		cfg = loaded

		var cancel context.CancelFunc
		ctx, cancel = signal.NotifyContext(
			context.Background(),
			syscall.SIGINT, syscall.SIGTERM,
		)
		_ = cancel

		idx = indexer.NewIndexer(cfg.ManticoreURL)
		if err := idx.CreateTable(ctx); err != nil {
			return fmt.Errorf("failed to create Manticore table: %w", err)
		}

		return nil
	},
}

// RequireTrackerEnv ensures the Tracker credentials are present. Subcommands
// that talk to the Yandex Tracker API (sync, serve) call this from their PreRunE.
func RequireTrackerEnv() error {
	return cfg.RequireTracker()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func init() {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose logging")

	// Generated from the config.Config tags so the help can't drift from
	// what is actually read.
	envHelp, err := config.Description()
	if err != nil {
		envHelp = "Environment variables: (описание недоступно: " + err.Error() + ")"
	}

	rootCmd.SetHelpTemplate(`{{.Long}}

Usage:
  {{.UseLine}}

Available Commands:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding}} {{.Short}}{{end}}{{end}}

Flags:
{{.LocalFlags.FlagUsages}}
Global Flags:
{{.InheritedFlags.FlagUsages}}
` + envHelp + `

Use "{{.CommandPath}} [command] --help" for more information about a command.
`)
}

func GetContext() context.Context  { return ctx }
func GetIndexer() *indexer.Indexer { return idx }
func GetConfig() *config.Config    { return cfg }

func GetTrackerClient() *tracker.Client {
	token, isIAM := cfg.TrackerAuth()
	scheme := tracker.AuthOAuth
	if isIAM {
		scheme = tracker.AuthIAM
	}
	return tracker.NewClientWithAuth(token, scheme, cfg.OrgID, cfg.AttachmentTextMaxBytes)
}

// mapOptions - map settings from the config. They live here, at the wiring
// point, because indexer shouldn't know a config exists.
func mapOptions() indexer.MapOptions {
	return indexer.MapOptions{
		MaxIssues:            cfg.MapMaxIssues,
		MaxFiles:             cfg.MapMaxFiles,
		MaxFileNamesPerIssue: cfg.MapMaxFileNames,
		MaxDocChars:          cfg.MapMaxDocChars,
		MaxVocab:             cfg.MapMaxVocab,
		MaxNeighbors:         cfg.MapMaxNeighbors,
		SimilarityDims:       cfg.MapSimilarityDims,
		ClusterK:             cfg.MapClusterK,
	}
}

func newSearchService(mgr *syncer.Manager) *searchapi.Service {
	return searchapi.NewService(idx, mgr, searchapi.Options{
		MapCacheTTL: cfg.MapCacheTTL,
		MapOptions:  mapOptions(),
	})
}
