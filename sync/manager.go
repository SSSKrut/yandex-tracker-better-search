package sync

import (
	"context"
	"fmt"
	"log"
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
	Duration      string    `json:"duration,omitempty"`
}

// Manager - synchronization manager
type Manager struct {
	tracker  *tracker.Client
	indexer  *indexer.Indexer
	queues   []string
	workers  int
	interval time.Duration

	mu             sync.RWMutex
	status         Status
	logs           []LogEntry
	requestChannel chan bool
}

// LogEntry - html log entry
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // info, error, warning
	Message string    `json:"message"`
}

// NewManager - creates sync manager instance
func NewManager(tracker *tracker.Client, indexer *indexer.Indexer, queues []string, workers int, interval time.Duration) *Manager {
	return &Manager{
		tracker:        tracker,
		indexer:        indexer,
		queues:         queues,
		workers:        workers,
		interval:       interval,
		logs:           make([]LogEntry, 0, 100),
		requestChannel: make(chan bool, 1),
	}
}

// Start - starts periodic synchronization
func (m *Manager) Start(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	syncCtx, cancel := context.WithCancel(ctx)

	go m.RunSync(syncCtx)

	for {
		select {
		case <-ctx.Done():
			m.addLog("info", "Sync manager stopped")
			cancel()
			return
		case <-ticker.C:
			syncCtx, cancel = context.WithCancel(ctx)
			go m.RunSync(syncCtx)

			m.addLog("info", "Scheduled sync triggered")
		case req := <-m.requestChannel:
			if req {
				syncCtx, cancel = context.WithCancel(ctx)
				go m.RunSync(syncCtx)
			} else {
				if cancel != nil {
					cancel()
				}
			}
		}
	}
}

// RunSync - starts synchronization
func (m *Manager) RunSync(ctx context.Context) {
	if m.GetStatus().InProgress {
		m.addLog("warning", "Sync already in progress, skipping")
		return
	}
	m.mu.Lock()
	m.status.InProgress = true
	m.status.LastSyncError = ""
	m.mu.Unlock()

	startTime := time.Now()
	m.addLog("info", "Starting sync...")

	defer func() {
		m.mu.Lock()
		m.status.InProgress = false
		m.mu.Unlock()
	}()

	issues, result, err := m.tracker.InitialSync(ctx, m.queues, m.workers)
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

	duration := time.Since(startTime)

	m.mu.Lock()
	m.status.LastSyncAt = time.Now()
	m.status.IssuesCount = result.TotalIssues
	m.status.CommentsCount = result.TotalComments
	m.status.Duration = duration.Round(time.Second).String()
	m.mu.Unlock()

	m.addLog("info", fmt.Sprintf("Sync completed: %d issues, %d comments in %s",
		result.TotalIssues, result.TotalComments, duration.Round(time.Second)))
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
