package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"
)

// FetchAllIssues - loads all issues from the specified queues (or all if queues is empty).
// Uses a scrolling mechanism for large datasets
func (c *Client) FetchAllIssues(ctx context.Context, queues []string) ([]Issue, error) {
	return c.fetchAllIssuesWithProgress(ctx, queues, nil)
}

func (c *Client) fetchAllIssuesWithProgress(ctx context.Context, queues []string, progress ProgressFunc) ([]Issue, error) {
	if progress == nil {
		progress = noopProgress
	}
	var allIssues []Issue

	queueQuery := buildQueueQuery(queues)
	var query string
	if queueQuery != "" {
		query = fmt.Sprintf(`(%s) "Sort By": Updated DESC`, queueQuery)
	} else {
		query = `"Sort By": Updated DESC`
	}

	reqBody := SearchRequest{Query: query}

	// first request with scroll initialization
	path := fmt.Sprintf("/issues/_search?scrollType=sorted&perScroll=%d", maxPerPage)

	page := 1
	for {
		select {
		case <-ctx.Done():
			return allIssues, ctx.Err()
		default:
		}

		log.Printf("Fetching issues page %d (loaded: %d)...", page, len(allIssues))

		respBody, headers, err := c.doRequest(ctx, "POST", path, reqBody)
		if err != nil {
			return allIssues, fmt.Errorf("fetch issues page %d: %w", page, err)
		}

		var issues []Issue
		if err := json.Unmarshal(respBody, &issues); err != nil {
			return allIssues, fmt.Errorf("unmarshal issues: %w", err)
		}

		allIssues = append(allIssues, issues...)
		// Scroll-based pagination: total is unknown until the last page, so
		// we report a count-only progress event (total=0).
		progress(ProgressStageIssues, len(allIssues), 0)

		// check for more pages
		scrollID := headers.Get("X-Scroll-Id")
		if scrollID == "" || len(issues) < maxPerPage {
			break
		}

		path = fmt.Sprintf("/issues/_search?scrollId=%s", scrollID)
		page++
	}

	log.Printf("Total issues fetched: %d", len(allIssues))
	progress(ProgressStageIssues, len(allIssues), len(allIssues))
	return allIssues, nil
}

// FetchIssueComments - loads all comments for the specified issue
func (c *Client) FetchIssueComments(ctx context.Context, issueKey string) ([]Comment, error) {
	var allComments []Comment

	page := 1
	for {
		select {
		case <-ctx.Done():
			return allComments, ctx.Err()
		default:
		}

		path := fmt.Sprintf("/issues/%s/comments?perPage=%d&page=%d", issueKey, maxPerPage, page)

		respBody, headers, err := c.doRequest(ctx, "GET", path, nil)
		if err != nil {
			return allComments, fmt.Errorf("fetch comments for %s page %d: %w", issueKey, page, err)
		}

		var comments []Comment
		if err := json.Unmarshal(respBody, &comments); err != nil {
			return allComments, fmt.Errorf("unmarshal comments: %w", err)
		}

		allComments = append(allComments, comments...)

		// check for more pages
		totalPages := headers.Get("X-Total-Pages")
		if totalPages == "" {
			break
		}

		totalPagesInt, err := strconv.Atoi(totalPages) // using Atoi because it's faster
		if err != nil {
			return allComments, fmt.Errorf("parse total pages: %w", err)
		}
		if page >= totalPagesInt {
			break
		}

		page++
	}

	return allComments, nil
}

// FetchIssueAttachments - loads all attachments for the specified issue
func (c *Client) FetchIssueAttachments(ctx context.Context, issueKey string) ([]Attachment, error) {
	var allAttachments []Attachment

	page := 1
	for {
		select {
		case <-ctx.Done():
			return allAttachments, ctx.Err()
		default:
		}

		path := fmt.Sprintf("/issues/%s/attachments?perPage=%d&page=%d", issueKey, maxPerPage, page)

		respBody, headers, err := c.doRequest(ctx, "GET", path, nil)
		if err != nil {
			return allAttachments, fmt.Errorf("fetch attachments for %s page %d: %w", issueKey, page, err)
		}

		var attachments []Attachment
		if err := json.Unmarshal(respBody, &attachments); err != nil {
			return allAttachments, fmt.Errorf("unmarshal attachments: %w", err)
		}

		allAttachments = append(allAttachments, attachments...)

		totalPages := headers.Get("X-Total-Pages")
		if totalPages == "" {
			break
		}

		totalPagesInt, err := strconv.Atoi(totalPages)
		if err != nil {
			return allAttachments, fmt.Errorf("parse total pages: %w", err)
		}
		if page >= totalPagesInt {
			break
		}

		page++
	}

	return allAttachments, nil
}

// FetchUpdatedIssues - loads issues updated since the specified timestamp (in RFC3339 format)
func (c *Client) FetchUpdatedIssues(ctx context.Context, since time.Time, queues []string) ([]Issue, error) {
	return c.fetchUpdatedIssuesWithProgress(ctx, since, queues, nil)
}

func (c *Client) fetchUpdatedIssuesWithProgress(ctx context.Context, since time.Time, queues []string, progress ProgressFunc) ([]Issue, error) {
	if progress == nil {
		progress = noopProgress
	}
	query := buildUpdatedQuery(since, queues)

	reqBody := SearchRequest{Query: query}

	var allIssues []Issue
	path := fmt.Sprintf("/issues/_search?perPage=%d&page=1", maxPerPage)

	page := 1
	for {
		select {
		case <-ctx.Done():
			return allIssues, ctx.Err()
		default:
		}

		respBody, headers, err := c.doRequest(ctx, "POST", path, reqBody)
		if err != nil {
			return allIssues, fmt.Errorf("fetch updated issues page %d: %w", page, err)
		}

		var issues []Issue
		if err := json.Unmarshal(respBody, &issues); err != nil {
			return allIssues, fmt.Errorf("unmarshal issues: %w", err)
		}

		allIssues = append(allIssues, issues...)

		// check for more pages
		totalPages := headers.Get("X-Total-Pages")
		if totalPages == "" {
			progress(ProgressStageIssues, len(allIssues), len(allIssues))
			break
		}

		totalPagesInt, _ := strconv.Atoi(totalPages)
		// We can derive a reasonable upper bound for total issues from
		// totalPages*maxPerPage; it'll over-estimate on the last page but
		// good enough for a circular indicator.
		estTotal := totalPagesInt * maxPerPage
		progress(ProgressStageIssues, len(allIssues), estTotal)
		if page >= totalPagesInt {
			progress(ProgressStageIssues, len(allIssues), len(allIssues))
			break
		}

		page++
		path = fmt.Sprintf("/issues/_search?perPage=%d&page=%d", maxPerPage, page)
	}

	return allIssues, nil
}

func buildUpdatedQuery(since time.Time, queues []string) string {
	sinceStr := since.Format("2006-01-02 15:04:05")
	updatedQuery := fmt.Sprintf(`Updated: >= "%s"`, sinceStr)
	queueQuery := buildQueueQuery(queues)
	if queueQuery != "" {
		return fmt.Sprintf(`(%s) AND (%s) "Sort By": Updated ASC`, updatedQuery, queueQuery)
	}
	return fmt.Sprintf(`%s "Sort By": Updated ASC`, updatedQuery)
}

func buildQueueQuery(queues []string) string {
	if len(queues) == 0 {
		return ""
	}
	query := fmt.Sprintf(`Queue: %s`, queues[0])
	for _, q := range queues[1:] {
		query = fmt.Sprintf(`(%s) OR Queue: %s`, query, q)
	}
	return query
}
