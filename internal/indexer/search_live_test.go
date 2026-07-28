package indexer

import (
	"context"
	"os"
	"testing"
)

// TestLive_SearchAcceptedByManticore - only the engine sees "P01/P08: syntax
// error"; string tests miss it. Runs when MANTICORE_TEST_URL is set:
//
//	MANTICORE_TEST_URL=http://localhost:9308 go test ./internal/indexer -run TestLive
func TestLive_SearchAcceptedByManticore(t *testing.T) {
	manticoreURL := os.Getenv("MANTICORE_TEST_URL")
	if manticoreURL == "" {
		t.Skip("set MANTICORE_TEST_URL to run against a live Manticore")
	}

	idx := NewIndexer(manticoreURL)
	ctx := context.Background()

	// A fresh instance (CI) has no tables and every SELECT fails with
	// "unknown table". IF NOT EXISTS, so this is a no-op on a live index.
	if err := idx.CreateTable(ctx); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	queries := []string{
		"",
		"https://",
		"https://tracker.yandex.ru/NOVA-42",
		"https://tracker.yandex.ru/NOVA-42?a=1#tail",
		"http://example.com/a.b/c?d=1&e=(2)",
		"www.example.com",
		"NOVA-42",
		"кнопка отчёт",
		"login error",
		`foo)`,
		`(((`,
		`"`,
		`'`,
		`\`,
		`@summary`,
		`a | b`,
		`-foo`,
		`***`,
		`%`,
		`?`,
		`~!^$[]{}<>=&:;,`,
		"C:\\Users\\test",
		"x'; DROP TABLE issues; --",
	}

	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			if _, err := idx.SearchWithFilters(ctx, query, SearchFilters{}, 10); err != nil {
				t.Errorf("search %q: %v", query, err)
			}
			if _, err := idx.SearchWithFilters(ctx, query, SearchFilters{Queue: "NOVA"}, 10); err != nil {
				t.Errorf("filtered search %q: %v", query, err)
			}
		})
	}
}
