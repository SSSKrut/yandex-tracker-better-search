package indexer

import (
	"context"
	"os"
	"testing"
)

// TestLive_SearchAcceptedByManticore - "P01/P08: syntax error" видны только движку,
// тесты на строках их не ловят. Запускается при заданном MANTICORE_TEST_URL:
//
//	MANTICORE_TEST_URL=http://localhost:9308 go test ./internal/indexer -run TestLive
func TestLive_SearchAcceptedByManticore(t *testing.T) {
	manticoreURL := os.Getenv("MANTICORE_TEST_URL")
	if manticoreURL == "" {
		t.Skip("set MANTICORE_TEST_URL to run against a live Manticore")
	}

	idx := NewIndexer(manticoreURL)
	ctx := context.Background()

	// На пустом инстансе (CI) таблиц нет и любой SELECT падает на "unknown table".
	// CREATE TABLE IF NOT EXISTS, так что на живой базе это no-op.
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
