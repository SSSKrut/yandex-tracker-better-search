package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
)

// FetchAllIssues - loads all issues from the specified queues (or all if queues is empty).
// Uses a scrolling mechanism for large datasets
func (c *Client) FetchAllIssues(ctx context.Context, queues []string) ([]Issue, error) {
	var allIssues []Issue

	var query string
	if len(queues) > 0 {
		query = fmt.Sprintf(`Queue: %s "Sort By": Updated DESC`, queues[0])
		for _, q := range queues[1:] {
			query = fmt.Sprintf(`(%s) OR Queue: %s`, query, q)
		}
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

		// check for more pages
		scrollID := headers.Get("X-Scroll-Id")
		if scrollID == "" || len(issues) < maxPerPage {
			break
		}

		path = fmt.Sprintf("/issues/_search?scrollId=%s", scrollID)
		page++
	}

	log.Printf("Total issues fetched: %d", len(allIssues))
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

// FetchUpdatedIssues - loads issues updated since the specified timestamp (in RFC3339 format)
func (c *Client) FetchUpdatedIssues(ctx context.Context, since string) ([]Issue, error) {
	query := fmt.Sprintf(`Updated: >= "%s" "Sort By": Updated ASC`, since)

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
			break
		}

		totalPagesInt, _ := strconv.Atoi(totalPages)
		if page >= totalPagesInt {
			break
		}

		page++
		path = fmt.Sprintf("/issues/_search?perPage=%d&page=%d", maxPerPage, page)
	}

	return allIssues, nil
}
