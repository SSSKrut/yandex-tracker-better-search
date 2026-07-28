package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type SyncState struct {
	LastFullSyncAt        time.Time `json:"last_full_sync_at"`
	LastIncrementalSyncAt time.Time `json:"last_incremental_sync_at"`
	LastUpdatedAt         time.Time `json:"last_updated_at"`
}

type StateStore struct {
	path string
}

// DefaultStatePath - the fallback path; SYNC_STATE_PATH overrides it via the
// config loaded in cmd/ytbs.
const DefaultStatePath = "backups/sync_state.json"

func NewStateStore(path string) *StateStore {
	return &StateStore{path: path}
}

func (s *StateStore) Load() (SyncState, error) {
	var state SyncState
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func (s *StateStore) Save(state SyncState) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
