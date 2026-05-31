package tracker

import (
	"context"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// IndexedIssue - issue prepared for indexing in Manticore
type IndexedIssue struct {
	ID           string    `json:"id"`
	Key          string    `json:"key"`
	URL          string    `json:"url"`
	Summary      string    `json:"summary"`
	Description  string    `json:"description"`
	CommentsText string    `json:"comments_text"`
	Queue        string    `json:"queue"`
	Status       string    `json:"status"`
	StatusName   string    `json:"status_name"`
	Priority     string    `json:"priority"`
	Type         string    `json:"type"`
	Resolution   string    `json:"resolution"`
	Author       string    `json:"author"`
	AuthorName   string    `json:"author_name"`
	Assignee     string    `json:"assignee"`
	AssigneeName string    `json:"assignee_name"`
	Tags         []string  `json:"tags"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SyncResult - synchronization result summary
type SyncResult struct {
	TotalIssues   int
	TotalComments int
	TotalFiles    int
	TextFiles     int
	ProcessedAt   time.Time
	MaxUpdatedAt  time.Time
	Errors        []error
}

// Sync stage names passed to ProgressFunc. They are intentionally short and
// stable since they're surfaced to the UI.
const (
	ProgressStageIssues   = "issues"
	ProgressStageComments = "comments"
)

// ProgressFunc receives incremental updates during a sync. total=0 means the
// upper bound is not yet known (e.g. during scrolled issue pagination). The
// callback is invoked from worker goroutines, so implementations must be
// safe for concurrent use.
type ProgressFunc func(stage string, current, total int)

func noopProgress(string, int, int) {}

// InitialSync - performs the initial synchronization: fetches all issues and their comments
func (c *Client) InitialSync(ctx context.Context, queues []string, workers int) ([]IndexedIssue, []IndexedFile, *SyncResult, error) {
	return c.InitialSyncWithProgress(ctx, queues, workers, nil)
}

// InitialSyncWithProgress is like InitialSync but emits progress events through
// the supplied callback. Pass nil to disable progress reporting.
func (c *Client) InitialSyncWithProgress(ctx context.Context, queues []string, workers int, progress ProgressFunc) ([]IndexedIssue, []IndexedFile, *SyncResult, error) {
	if progress == nil {
		progress = noopProgress
	}
	log.Println("Starting full sync...")
	issues, err := c.fetchAllIssuesWithProgress(ctx, queues, progress)
	if err != nil {
		return nil, nil, &SyncResult{ProcessedAt: time.Now()}, err
	}

	return c.buildIndexed(ctx, issues, workers, progress)
}

// IncrementalSync - performs synchronization for issues updated since a timestamp
func (c *Client) IncrementalSync(ctx context.Context, since time.Time, queues []string, workers int) ([]IndexedIssue, []IndexedFile, *SyncResult, error) {
	return c.IncrementalSyncWithProgress(ctx, since, queues, workers, nil)
}

// IncrementalSyncWithProgress is like IncrementalSync but emits progress
// events through the supplied callback. Pass nil to disable progress reporting.
func (c *Client) IncrementalSyncWithProgress(ctx context.Context, since time.Time, queues []string, workers int, progress ProgressFunc) ([]IndexedIssue, []IndexedFile, *SyncResult, error) {
	if progress == nil {
		progress = noopProgress
	}
	log.Printf("Starting incremental sync since %s...", since.Format("2006-01-02 15:04:05"))
	issues, err := c.fetchUpdatedIssuesWithProgress(ctx, since, queues, progress)
	if err != nil {
		return nil, nil, &SyncResult{ProcessedAt: time.Now()}, err
	}
	return c.buildIndexed(ctx, issues, workers, progress)
}

func (c *Client) buildIndexed(ctx context.Context, issues []Issue, workers int, progress ProgressFunc) ([]IndexedIssue, []IndexedFile, *SyncResult, error) {
	if progress == nil {
		progress = noopProgress
	}
	result := &SyncResult{ProcessedAt: time.Now()}
	result.TotalIssues = len(issues)
	if len(issues) == 0 {
		return nil, nil, result, nil
	}

	log.Printf("Fetched %d issues, loading comments...", len(issues))
	progress(ProgressStageComments, 0, len(issues))

	if workers <= 0 {
		workers = 5
	}

	type issueWithComments struct {
		issue    Issue
		comments []Comment
		files    []IndexedFile
		errors   []error
		err      error
	}

	jobs := make(chan Issue, len(issues))
	results := make(chan issueWithComments, len(issues))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for issue := range jobs {
				comments, err := c.FetchIssueComments(ctx, issue.Key)
				files, fileErrors := c.extractIndexedFilesForIssue(ctx, issue, comments)
				results <- issueWithComments{
					issue:    issue,
					comments: comments,
					files:    files,
					errors:   fileErrors,
					err:      err,
				}
			}
		}()
	}

	go func() {
		for _, issue := range issues {
			jobs <- issue
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var indexed []IndexedIssue
	var indexedFiles []IndexedFile
	processed := 0

	for r := range results {
		processed++
		if processed%100 == 0 {
			log.Printf("Processing comments: %d/%d", processed, len(issues))
		}
		progress(ProgressStageComments, processed, len(issues))

		if r.err != nil {
			result.Errors = append(result.Errors, r.err)
			log.Printf("Error fetching comments for issue %s: %v", r.issue.Key, r.err)
		}

		if len(r.errors) > 0 {
			result.Errors = append(result.Errors, r.errors...)
		}

		if r.issue.UpdatedAt.Time.After(result.MaxUpdatedAt) {
			result.MaxUpdatedAt = r.issue.UpdatedAt.Time
		}

		result.TotalComments += len(r.comments)
		result.TotalFiles += len(r.files)

		for _, file := range r.files {
			if file.IsText {
				result.TextFiles++
			}
		}

		indexed = append(indexed, convertToIndexed(r.issue, r.comments))
		indexedFiles = append(indexedFiles, r.files...)
	}

	log.Printf("Sync completed: %d issues, %d comments, %d files (%d text), %d errors",
		result.TotalIssues, result.TotalComments, result.TotalFiles, result.TextFiles, len(result.Errors))

	return indexed, indexedFiles, result, nil
}

// convertToIndexed - converts Issue and its comments to IndexedIssue
func convertToIndexed(issue Issue, comments []Comment) IndexedIssue {
	indexed := IndexedIssue{
		ID:          issue.ID,
		Key:         issue.Key,
		URL:         "https://tracker.yandex.ru/" + issue.Key,
		Summary:     issue.Summary,
		Description: stripHTML(issue.Description),
		Queue:       issue.Queue.Key,
		Status:      issue.Status.Key,
		StatusName:  issue.Status.Display,
		Priority:    issue.Priority.Key,
		Type:        issue.Type.Key,
		Author:      issue.Author.ID,
		AuthorName:  issue.Author.Display,
		Tags:        issue.Tags,
		CreatedAt:   issue.CreatedAt.Time,
		UpdatedAt:   issue.UpdatedAt.Time,
	}

	if issue.Resolution != nil {
		indexed.Resolution = issue.Resolution.Key
	}

	if issue.Assignee != nil {
		indexed.Assignee = issue.Assignee.ID
		indexed.AssigneeName = issue.Assignee.Display
	}

	// combine comments text
	var commentTexts []string
	for _, c := range comments {
		text := stripHTML(c.Text)
		if text != "" {
			commentTexts = append(commentTexts, text)
		}
	}
	indexed.CommentsText = strings.Join(commentTexts, "\n\n")

	return indexed
}

// stripHTML - removes HTML tags from a string
func stripHTML(s string) string {
	// TODO: check for library for more robust HTML stripping
	re := regexp.MustCompile(`<[^>]*>`)
	s = re.ReplaceAllString(s, "")

	// decode common HTML entities
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")

	// remove extra spaces
	s = strings.TrimSpace(s)

	return s
}
