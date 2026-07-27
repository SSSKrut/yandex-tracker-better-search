// Package searchapi exposes the high-level read/write operations that ytbs
// offers to its frontends (HTTP server, MCP server, CLI). It wraps the lower
// level *indexer.Indexer and *sync.Manager so transports stay thin.
package searchapi

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	stdsync "sync"
	"time"

	"github.com/SSSKrut/yandex-tracker-better-search/indexer"
	syncer "github.com/SSSKrut/yandex-tracker-better-search/sync"
)

// DefaultTruncateChars caps description and comments length when GetIssue is
// called with full=false. Picked to fit ~500 tokens into a Claude context
// window without flooding it.
const DefaultTruncateChars = 2000

// Service is the transport-agnostic API surface. It is safe for concurrent use.
type Service struct {
	indexer     *indexer.Indexer
	syncManager *syncer.Manager

	mapCacheMu  stdsync.Mutex
	mapCache    *indexer.MapData
	mapCacheAt  time.Time
	mapCacheTTL time.Duration
}

// NewService constructs a Service. mapCacheTTL of 0 falls back to MAP_CACHE_MINUTES
// or a 10-minute default.
func NewService(idx *indexer.Indexer, mgr *syncer.Manager) *Service {
	return &Service{
		indexer:     idx,
		syncManager: mgr,
		mapCacheTTL: mapCacheTTLFromEnv(),
	}
}

func mapCacheTTLFromEnv() time.Duration {
	const defaultMinutes = 10
	val := strings.TrimSpace(os.Getenv("MAP_CACHE_MINUTES"))
	if val == "" {
		return time.Duration(defaultMinutes) * time.Minute
	}
	parsed, err := strconv.Atoi(val)
	if err != nil || parsed <= 0 {
		return time.Duration(defaultMinutes) * time.Minute
	}
	return time.Duration(parsed) * time.Minute
}

// SearchParams aggregates the inputs accepted by Search. Empty fields mean "no
// filter". Limit is clamped to [1, 100]; default is 20.
type SearchParams struct {
	Query    string
	Queue    string
	Status   string
	Priority string
	Author   string
	Assignee string
	Limit    int
}

// SearchHit is the compact representation returned by Search. It intentionally
// omits highlight/description so list responses stay small for LLM contexts.
type SearchHit struct {
	Kind         string    `json:"kind"`
	Key          string    `json:"key"`
	URL          string    `json:"url"`
	Summary      string    `json:"summary"`
	StatusName   string    `json:"status_name,omitempty"`
	AssigneeName string    `json:"assignee_name,omitempty"`
	Queue        string    `json:"queue,omitempty"`
	Priority     string    `json:"priority,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	FileName     string    `json:"file_name,omitempty"`
	MimeType     string    `json:"mime_type,omitempty"`
}

// Search runs a full-text search with optional filters and returns a compact
// list of hits.
func (s *Service) Search(ctx context.Context, p SearchParams) ([]SearchHit, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	results, err := s.indexer.SearchWithFilters(ctx, p.Query, indexer.SearchFilters{
		Queue:    p.Queue,
		Status:   p.Status,
		Priority: p.Priority,
		Author:   p.Author,
		Assignee: p.Assignee,
	}, limit)
	if err != nil {
		return nil, err
	}

	hits := make([]SearchHit, 0, len(results))
	for _, r := range results {
		hits = append(hits, SearchHit{
			Kind:         r.Kind,
			Key:          r.Key,
			URL:          r.URL,
			Summary:      r.Summary,
			StatusName:   r.StatusName,
			AssigneeName: r.AssigneeName,
			Queue:        r.Queue,
			Priority:     r.Priority,
			UpdatedAt:    r.UpdatedAt,
			FileName:     r.FileName,
			MimeType:     r.MimeType,
		})
	}
	return hits, nil
}

// AttachmentInfo is the lightweight attachment representation shipped inside
// IssueDetail. URL points to the issue UI page (Tracker has no public per-file
// URL — see tracker/attachments.go).
type AttachmentInfo struct {
	FileName string `json:"file_name"`
	URL      string `json:"url"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
	IsText   bool   `json:"is_text"`
	Source   string `json:"source,omitempty"`
}

// IssueDetail is the full record returned by GetIssue. When Truncated is true,
// Description and CommentsText were shortened — call GetIssue with full=true
// to receive the untruncated payload.
type IssueDetail struct {
	Key          string           `json:"key"`
	URL          string           `json:"url"`
	Summary      string           `json:"summary"`
	Description  string           `json:"description"`
	CommentsText string           `json:"comments_text"`
	StatusName   string           `json:"status_name,omitempty"`
	AssigneeName string           `json:"assignee_name,omitempty"`
	AuthorName   string           `json:"author_name,omitempty"`
	Queue        string           `json:"queue,omitempty"`
	Priority     string           `json:"priority,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	Attachments  []AttachmentInfo `json:"attachments"`
	Truncated    bool             `json:"truncated"`
}

// ErrIssueNotFound is returned by GetIssue when no issue exists with the given
// key.
var ErrIssueNotFound = indexer.ErrIssueNotFound

// GetIssue returns the full issue record. When full=false, Description and
// CommentsText are capped at DefaultTruncateChars and Truncated is set to true
// if any field was shortened.
func (s *Service) GetIssue(ctx context.Context, key string, full bool) (*IssueDetail, error) {
	rec, err := s.indexer.GetIssueByKey(ctx, key)
	if err != nil {
		return nil, err
	}

	files, err := s.indexer.GetFilesByIssueKey(ctx, key)
	if err != nil {
		return nil, err
	}

	desc := rec.Description
	comments := rec.CommentsText
	truncated := false
	if !full {
		if shortened, was := truncateRunes(desc, DefaultTruncateChars); was {
			desc = shortened
			truncated = true
		}
		if shortened, was := truncateRunes(comments, DefaultTruncateChars); was {
			comments = shortened
			truncated = true
		}
	}

	atts := make([]AttachmentInfo, 0, len(files))
	for _, f := range files {
		atts = append(atts, AttachmentInfo{
			FileName: f.FileName,
			URL:      f.IssueURL,
			MimeType: f.MimeType,
			Size:     f.Size,
			IsText:   f.IsText,
			Source:   f.Source,
		})
	}

	return &IssueDetail{
		Key:          rec.Key,
		URL:          rec.URL,
		Summary:      rec.Summary,
		Description:  desc,
		CommentsText: comments,
		StatusName:   rec.StatusName,
		AssigneeName: rec.AssigneeName,
		AuthorName:   rec.AuthorName,
		Queue:        rec.Queue,
		Priority:     rec.Priority,
		CreatedAt:    rec.CreatedAt,
		UpdatedAt:    rec.UpdatedAt,
		Attachments:  atts,
		Truncated:    truncated,
	}, nil
}

// truncateRunes shortens s to maxChars runes (not bytes) and appends an
// ellipsis marker. Returns (s, false) when the input already fits.
func truncateRunes(s string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		return s, false
	}
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s, false
	}
	return string(runes[:maxChars]) + "… [truncated]", true
}

// Neighbor pairs a tracker key with a similarity score from the 2D map.
type Neighbor struct {
	Key   string  `json:"key"`
	Title string  `json:"title"`
	URL   string  `json:"url"`
	Kind  string  `json:"kind"`
	Score float64 `json:"score"`
}

// ErrNotInMap is returned by Neighbors when the requested key did not make it
// into the similarity map (e.g. fell outside MAP_MAX_ISSUES).
var ErrNotInMap = errors.New("issue not in similarity map")

// Neighbors returns up to k nearest documents to the issue identified by key,
// ordered by descending similarity. The 2D map is built lazily on the first
// call and refreshed when the cache TTL expires.
func (s *Service) Neighbors(ctx context.Context, key string, k int) ([]Neighbor, error) {
	if key == "" {
		return nil, ErrNotInMap
	}
	if k <= 0 {
		k = 5
	}

	data, err := s.getOrBuildMap(ctx, false)
	if err != nil {
		return nil, err
	}

	var target *indexer.MapPoint
	pointByID := make(map[string]*indexer.MapPoint, len(data.Points))
	for i := range data.Points {
		p := &data.Points[i]
		pointByID[p.ID] = p
		if p.Kind == "issue" && p.Key == key {
			target = p
		}
	}
	if target == nil {
		return nil, ErrNotInMap
	}

	out := make([]Neighbor, 0, k)
	for _, n := range target.Neighbors {
		if len(out) >= k {
			break
		}
		other, ok := pointByID[n.ID]
		if !ok {
			continue
		}
		out = append(out, Neighbor{
			Key:   other.Key,
			Title: other.Title,
			URL:   other.URL,
			Kind:  other.Kind,
			Score: n.Score,
		})
	}
	return out, nil
}

// ClusterSummary mirrors indexer.MapCluster for transport responses.
type ClusterSummary struct {
	ID          int      `json:"id"`
	Size        int      `json:"size"`
	TopKeywords []string `json:"top_keywords"`
	CentralKeys []string `json:"central_keys"`
}

// MapOverview returns a list of clusters with size, top keywords and a few
// central tracker keys per cluster, ordered by size DESC.
func (s *Service) MapOverview(ctx context.Context) ([]ClusterSummary, error) {
	data, err := s.getOrBuildMap(ctx, false)
	if err != nil {
		return nil, err
	}

	out := make([]ClusterSummary, 0, len(data.Clusters))
	for _, c := range data.Clusters {
		out = append(out, ClusterSummary{
			ID:          c.ID,
			Size:        c.Size,
			TopKeywords: c.TopKeywords,
			CentralKeys: c.CentralKeys,
		})
	}
	// Largest clusters first — agents typically want a high-level view.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Size > out[i].Size {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// Map returns the cached MapData (used by the HTTP /api/map endpoint). Pass
// refresh=true to force a rebuild.
func (s *Service) Map(ctx context.Context, refresh bool) (*indexer.MapData, error) {
	return s.getOrBuildMap(ctx, refresh)
}

// MapBuiltAt reports when the in-memory map cache was last populated, or the
// zero time if it has never been built.
func (s *Service) MapBuiltAt() time.Time {
	s.mapCacheMu.Lock()
	defer s.mapCacheMu.Unlock()
	return s.mapCacheAt
}

func (s *Service) getOrBuildMap(ctx context.Context, refresh bool) (*indexer.MapData, error) {
	if !refresh {
		s.mapCacheMu.Lock()
		cached := s.mapCache
		cachedAt := s.mapCacheAt
		s.mapCacheMu.Unlock()
		if cached != nil && time.Since(cachedAt) < s.mapCacheTTL {
			return cached, nil
		}
	}

	data, err := s.indexer.BuildSimilarityMap(ctx, indexer.MapOptionsFromEnv())
	if err != nil {
		return nil, err
	}

	s.mapCacheMu.Lock()
	s.mapCache = data
	s.mapCacheAt = time.Now()
	s.mapCacheMu.Unlock()

	return data, nil
}

// TriggerSync requests a manual sync. mode is either syncer.ModeIncremental
// or syncer.ModeFull; empty/unknown defaults to ModeFull.
func (s *Service) TriggerSync(mode string) error {
	return s.syncManager.TriggerSync(mode)
}

// CancelSync cancels an in-flight sync.
func (s *Service) CancelSync() error {
	return s.syncManager.CancelSync()
}

// FullStatus combines runtime sync status with persisted state and the map
// cache timestamp — everything an MCP/HTTP status endpoint typically wants.
type FullStatus struct {
	syncer.Status
	LastFullSyncAt        time.Time `json:"last_full_sync_at"`
	LastIncrementalSyncAt time.Time `json:"last_incremental_sync_at"`
	MapBuiltAt            time.Time `json:"map_built_at,omitempty"`
}

// Status returns a snapshot of the manager's status plus persisted state.
func (s *Service) Status() FullStatus {
	state := s.syncManager.GetState()
	return FullStatus{
		Status:                s.syncManager.GetStatus(),
		LastFullSyncAt:        state.LastFullSyncAt,
		LastIncrementalSyncAt: state.LastIncrementalSyncAt,
		MapBuiltAt:            s.MapBuiltAt(),
	}
}

// FilterOptions is a re-export so callers don't have to depend on indexer.
type FilterOptions = indexer.FilterOptions

// GetFilterOptions returns the unique values used to populate filter
// dropdowns in the UI.
func (s *Service) GetFilterOptions(ctx context.Context) (*FilterOptions, error) {
	return s.indexer.GetFilterOptions(ctx)
}

// Logs returns up to limit recent sync log entries.
func (s *Service) Logs(limit int) []syncer.LogEntry {
	return s.syncManager.GetLogs(limit)
}

// IndexerSearchResult is a re-export used by handlers that still need the
// rich row (e.g. with HTML highlight) for templating.
type IndexerSearchResult = indexer.SearchResult

// SearchRich returns the raw indexer.SearchResult slice (with highlight) used
// by the HTML UI. New callers should prefer Search, which strips highlight to
// keep responses small.
func (s *Service) SearchRich(ctx context.Context, p SearchParams) ([]IndexerSearchResult, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	return s.indexer.SearchWithFilters(ctx, p.Query, indexer.SearchFilters{
		Queue:    p.Queue,
		Status:   p.Status,
		Priority: p.Priority,
		Author:   p.Author,
		Assignee: p.Assignee,
	}, limit)
}

