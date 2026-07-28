package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"
)

var (
	searchLimit int
	searchJSON  bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search for issues (CLI mode)",
	Long: `Search indexed issues using full-text search.
    
Examples:
  ytbs search "authentication bug"
  ytbs search --limit 10 "login error"
  ytbs search "performance" -n 5`,

	Args: cobra.MinimumNArgs(1),

	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")

		log.Printf("Searching for: %s", query)

		results, err := GetIndexer().Search(GetContext(), query, searchLimit)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			log.Println("No results found")
			return nil
		}

		log.Printf("Found %d results:\n", len(results))

		for i, r := range results {
			if r.Kind == "file" {
				fmt.Printf("%d. [FILE:%s] %s\n", i+1, r.Key, r.FileName)
				fmt.Printf("   MIME: %s | Size: %d | Source: %s\n", r.MimeType, r.Size, r.Source)
				fmt.Printf("   Parent issue: %s\n", r.Key)
			} else {
				fmt.Printf("%d. [%s] %s\n", i+1, r.Key, r.Summary)
				fmt.Printf("   Status: %s | Assignee: %s\n", r.StatusName, r.AssigneeName)
			}
			fmt.Printf("   URL: %s\n", r.URL)
			if r.Highlight != "" {
				fmt.Printf("   Match: %s\n", r.Highlight)
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 20, "Maximum number of results")
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "Output results as JSON")

	rootCmd.AddCommand(searchCmd)
}
