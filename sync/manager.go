package sync

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"ytbs/indexer"
	"ytbs/tracker"
)

// Status - synchronization status
type Status struct {
	InProgress    bool      `json:"in_progress"`
	LastSyncAt    time.Time `json:"last_sync_at"`
	LastSyncError string    `json:"last_sync_error,omitempty"`
	IssuesCount   int       `json:"issues_count"`
	CommentsCount int       `json:"comments_count"`
	FilesCount    int       `json:"files_count"`
	TextFiles     int       `json:"text_files"`
	Duration      string    `json:"duration,omitempty"`
}

// Manager - synchronization manager
type Manager struct {
	tracker             *tracker.Client
	indexer             *indexer.Indexer
	queues              []string
	workers             int
	incrementalInterval time.Duration
	fullInterval        time.Duration
	overlap             time.Duration
	stateStore          *StateStore

	mu             sync.RWMutex
	status         Status
	logs           []LogEntry
	requestChannel chan bool
	state          SyncState
}

// LogEntry - html log entry
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // info, error, warning
	Message string    `json:"message"`
}

// NewManager - creates sync manager instance
func NewManager(tracker *tracker.Client, indexer *indexer.Indexer, queues []string, workers int, incrementalInterval, fullInterval time.Duration) *Manager {
	if incrementalInterval <= 0 {
		incrementalInterval = 15 * time.Minute
	}
	if fullInterval <= 0 {
		fullInterval = 24 * time.Hour
	}

	return &Manager{
		tracker:             tracker,
		indexer:             indexer,
		queues:              queues,
		workers:             workers,
		incrementalInterval: incrementalInterval,
		fullInterval:        fullInterval,
		overlap:             readEnvDuration("SYNC_OVERLAP", 2*time.Minute),
		stateStore:          NewStateStore(DefaultStatePath()),
		logs:                make([]LogEntry, 0, 100),
		requestChannel:      make(chan bool, 1),
	}
}

// Start - starts periodic synchronization
func (m *Manager) Start(ctx context.Context) {
	m.loadState()

	incTicker := time.NewTicker(m.incrementalInterval)
	fullTicker := time.NewTicker(m.fullInterval)
	defer incTicker.Stop()
	defer fullTicker.Stop()

	syncCtx, cancel := context.WithCancel(ctx)
	if m.state.LastFullSyncAt.IsZero() {
		go m.RunFullSync(syncCtx)
	} else {
		go m.RunIncrementalSync(syncCtx)
	}

	for {
		select {
		case <-ctx.Done():
			m.addLog("info", "Sync manager stopped")
			if cancel != nil {
				cancel()
			}
			return
		case <-incTicker.C:
			syncCtx, cancel = context.WithCancel(ctx)
			go m.RunIncrementalSync(syncCtx)
			m.addLog("info", "Scheduled incremental sync triggered")
		case <-fullTicker.C:
			syncCtx, cancel = context.WithCancel(ctx)
			go m.RunFullSync(syncCtx)
			m.addLog("info", "Scheduled full sync triggered")
		case req := <-m.requestChannel:
			if req {
				syncCtx, cancel = context.WithCancel(ctx)
				go m.RunFullSync(syncCtx)
			} else if cancel != nil {
				cancel()
			}
		}
	}
}

// RunFullSync - starts full synchronization
func (m *Manager) RunFullSync(ctx context.Context) {
	if m.GetStatus().InProgress {
		m.addLog("warning", "Sync already in progress, skipping")
		return
	}
	m.mu.Lock()
	m.status.InProgress = true
	m.status.LastSyncError = ""
	m.mu.Unlock()

	startTime := time.Now()
	m.addLog("info", "Starting full sync...")

	defer func() {
		m.mu.Lock()
		m.status.InProgress = false
		m.mu.Unlock()
	}()

	issues, files, result, err := m.tracker.InitialSync(ctx, m.queues, m.workers)
	if err != nil {
		m.mu.Lock()
		m.status.LastSyncError = err.Error()
		m.mu.Unlock()
		m.addLog("error", fmt.Sprintf("Sync failed: %v", err))
		return
	}

	if err := m.indexer.IndexIssues(ctx, issues); err != nil {
		m.mu.Lock()
		m.status.LastSyncError = err.Error()
		m.mu.Unlock()
		m.addLog("error", fmt.Sprintf("Indexing failed: %v", err))
		return
	}

	if err := m.indexer.IndexFiles(ctx, files); err != nil {
		m.mu.Lock()
		m.status.LastSyncError = err.Error()
		m.mu.Unlock()
		m.addLog("error", fmt.Sprintf("File indexing failed: %v", err))
		return
	}

	duration := time.Since(startTime)

	m.updateState(func(state *SyncState) {
		now := time.Now()
		state.LastFullSyncAt = now
		state.LastIncrementalSyncAt = now
		if result.MaxUpdatedAt.After(state.LastUpdatedAt) {
			state.LastUpdatedAt = result.MaxUpdatedAt
		}
	})

	m.mu.Lock()
	m.status.LastSyncAt = time.Now()
	m.status.IssuesCount = result.TotalIssues
	m.status.CommentsCount = result.TotalComments
	m.status.FilesCount = result.TotalFiles
	m.status.TextFiles = result.TextFiles
	m.status.Duration = duration.Round(time.Second).String()
	m.mu.Unlock()

	m.addLog("info", fmt.Sprintf("Full sync completed: %d issues, %d comments, %d files (%d text) in %s",
		result.TotalIssues, result.TotalComments, result.TotalFiles, result.TextFiles, duration.Round(time.Second)))
}

// RunIncrementalSync - starts synchronization for updates since last watermark
func (m *Manager) RunIncrementalSync(ctx context.Context) {
	if m.GetStatus().InProgress {
		m.addLog("warning", "Sync already in progress, skipping")
		return
	}

	state := m.getState()
	if state.LastUpdatedAt.IsZero() {
		m.addLog("warning", "No last sync timestamp found, running full sync")
		m.RunFullSync(ctx)
		return
	}

	m.mu.Lock()
	m.status.InProgress = true
	m.status.LastSyncError = ""
	m.mu.Unlock()

	startTime := time.Now()
	m.addLog("info", fmt.Sprintf("Starting incremental sync since %s...", state.LastUpdatedAt.Format("2006-01-02 15:04:05")))

	defer func() {
		m.mu.Lock()
		m.status.InProgress = false
		m.mu.Unlock()
	}()

	since := state.LastUpdatedAt.Add(-m.overlap)
	issues, files, result, err := m.tracker.IncrementalSync(ctx, since, m.queues, m.workers)
	if err != nil {
		m.mu.Lock()
		m.status.LastSyncError = err.Error()
		m.mu.Unlock()
		m.addLog("error", fmt.Sprintf("Incremental sync failed: %v", err))
		return
	}

	if err := m.indexer.IndexIssues(ctx, issues); err != nil {
		m.mu.Lock()
		m.status.LastSyncError = err.Error()
		m.mu.Unlock()
		m.addLog("error", fmt.Sprintf("Indexing failed: %v", err))
		return
	}

	if err := m.indexer.IndexFiles(ctx, files); err != nil {
		m.mu.Lock()
		m.status.LastSyncError = err.Error()
		m.mu.Unlock()
		m.addLog("error", fmt.Sprintf("File indexing failed: %v", err))
		return
	}

	m.updateState(func(state *SyncState) {
		state.LastIncrementalSyncAt = time.Now()
		if result.MaxUpdatedAt.After(state.LastUpdatedAt) {
			state.LastUpdatedAt = result.MaxUpdatedAt
		}
	})

	duration := time.Since(startTime)

	m.mu.Lock()
	m.status.LastSyncAt = time.Now()
	m.status.IssuesCount = result.TotalIssues
	m.status.CommentsCount = result.TotalComments
	m.status.FilesCount = result.TotalFiles
	m.status.TextFiles = result.TextFiles
	m.status.Duration = duration.Round(time.Second).String()
	m.mu.Unlock()

	m.addLog("info", fmt.Sprintf("Incremental sync completed: %d issues, %d comments, %d files (%d text) in %s",
		result.TotalIssues, result.TotalComments, result.TotalFiles, result.TextFiles, duration.Round(time.Second)))
}

// TriggerSync - starts synchronization manually
func (m *Manager) TriggerSync() error {
	if m.status.InProgress {
		return fmt.Errorf("sync already in progress")
	}

	m.requestChannel <- true
	m.addLog("info", "Manual sync triggered")
	return nil
}

// CancelSync - cancels current synchronization
func (m *Manager) CancelSync() error {
	if !m.status.InProgress {
		return fmt.Errorf("sync not in progress")
	}
	m.requestChannel <- false
	m.addLog("warning", "Sync cancelled by user")
	return nil
}

// GetStatus - returns current status
func (m *Manager) GetStatus() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// GetLogs - returns logs
func (m *Manager) GetLogs(limit int) []LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.logs) {
		limit = len(m.logs)
	}

	start := len(m.logs) - limit
	if start < 0 {
		start = 0
	}

	result := make([]LogEntry, limit)
	copy(result, m.logs[start:])

	// order reversing
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

func (m *Manager) addLog(level, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := LogEntry{
		Time:    time.Now(),
		Level:   level,
		Message: message,
	}

	m.logs = append(m.logs, entry)

	if len(m.logs) > 1000 {
		m.logs = m.logs[:999]
	}

	log.Printf("[%s] %s", level, message)
}

func (m *Manager) loadState() {
	if m.stateStore == nil {
		return
	}
	state, err := m.stateStore.Load()
	if err != nil {
		m.addLog("error", fmt.Sprintf("State load failed: %v", err))
		return
	}
	m.mu.Lock()
	m.state = state
	if !state.LastIncrementalSyncAt.IsZero() {
		m.status.LastSyncAt = state.LastIncrementalSyncAt
	} else if !state.LastFullSyncAt.IsZero() {
		m.status.LastSyncAt = state.LastFullSyncAt
	}
	m.mu.Unlock()
}

func (m *Manager) getState() SyncState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Manager) updateState(update func(state *SyncState)) {
	m.mu.Lock()
	update(&m.state)
	state := m.state
	m.mu.Unlock()

	if m.stateStore == nil {
		return
	}
	if err := m.stateStore.Save(state); err != nil {
		m.addLog("error", fmt.Sprintf("State save failed: %v", err))
	}
}

func readEnvDuration(name string, fallback time.Duration) time.Duration {
	val := strings.TrimSpace(os.Getenv(name))
	if val == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(val)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
