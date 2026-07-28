package sync

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStateStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewStateStore(path)

	state := SyncState{
		LastFullSyncAt:        time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		LastIncrementalSyncAt: time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC),
		LastUpdatedAt:         time.Date(2026, 5, 2, 11, 30, 0, 0, time.UTC),
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if !loaded.LastFullSyncAt.Equal(state.LastFullSyncAt) {
		t.Fatalf("LastFullSyncAt mismatch")
	}
	if !loaded.LastIncrementalSyncAt.Equal(state.LastIncrementalSyncAt) {
		t.Fatalf("LastIncrementalSyncAt mismatch")
	}
	if !loaded.LastUpdatedAt.Equal(state.LastUpdatedAt) {
		t.Fatalf("LastUpdatedAt mismatch")
	}
}

func TestDefaultStatePath(t *testing.T) {
	// Overridden through the config now, not by reading the environment here.
	if DefaultStatePath == "" {
		t.Fatal("DefaultStatePath must not be empty")
	}
}
