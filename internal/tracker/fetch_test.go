package tracker

import (
	"strings"
	"testing"
	"time"
)

func TestBuildQueueQuery_MultipleQueues(t *testing.T) {
	query := buildQueueQuery([]string{"DEV", "OPS"})
	if !strings.Contains(query, "Queue: DEV") || !strings.Contains(query, "Queue: OPS") {
		t.Fatalf("unexpected queue query: %q", query)
	}
	if !strings.Contains(query, "OR") {
		t.Fatalf("expected OR in queue query: %q", query)
	}
}

func TestBuildUpdatedQuery_WithQueues(t *testing.T) {
	since := time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC)
	query := buildUpdatedQuery(since, []string{"DEV"})

	if !strings.Contains(query, "Updated: >= \"2026-05-01 12:30:00\"") {
		t.Fatalf("expected formatted timestamp, got %q", query)
	}
	if !strings.Contains(query, "Queue: DEV") {
		t.Fatalf("expected queue filter, got %q", query)
	}
	if !strings.Contains(query, "\"Sort By\": Updated ASC") {
		t.Fatalf("expected sort clause, got %q", query)
	}
}
