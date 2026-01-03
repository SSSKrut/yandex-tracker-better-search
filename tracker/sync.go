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
	ProcessedAt   time.Time
	Errors        []error
}

// InitialSync - performs the initial synchronization: fetches all issues and their comments
func (c *Client) InitialSync(ctx context.Context, queues []string, workers int) ([]IndexedIssue, *SyncResult, error) {
	result := &SyncResult{
		ProcessedAt: time.Now(),
	}

	// 1. Load all issues
	log.Println("Starting initial sync...")
	issues, err := c.FetchAllIssues(ctx, queues)
	if err != nil {
		return nil, result, err
	}
	result.TotalIssues = len(issues)
	log.Printf("Fetched %d issues, loading comments...", len(issues))

	// 2. Load comments for issues with concurrency
	if workers <= 0 {
		workers = 5
	}

	type issueWithComments struct {
		issue    Issue
		comments []Comment
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
				results <- issueWithComments{
					issue:    issue,
					comments: comments,
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

	// 3. Collect results and convert to IndexedIssue
	var indexed []IndexedIssue
	processed := 0

	for r := range results {
		processed++
		if processed%100 == 0 {
			log.Printf("Processing comments: %d/%d", processed, len(issues))
		}

		if r.err != nil {
			result.Errors = append(result.Errors, r.err)
			log.Printf("Error fetching comments for issue %s: %v", r.issue.Key, r.err)
		}

		result.TotalComments += len(r.comments)

		indexed = append(indexed, convertToIndexed(r.issue, r.comments))
	}

	log.Printf("Initial sync completed: %d issues, %d comments, %d errors",
		result.TotalIssues, result.TotalComments, len(result.Errors))

	return indexed, result, nil
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
