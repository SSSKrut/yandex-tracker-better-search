package indexer

import (
	"context"
	"fmt"
	"time"
)

// IssueRecord - full issue row used for detail views (includes description and comments).
type IssueRecord struct {
	ID           string    `json:"id"`
	Key          string    `json:"key"`
	URL          string    `json:"url"`
	Summary      string    `json:"summary"`
	Description  string    `json:"description"`
	CommentsText string    `json:"comments_text"`
	StatusName   string    `json:"status_name"`
	AssigneeName string    `json:"assignee_name"`
	AuthorName   string    `json:"author_name"`
	Queue        string    `json:"queue"`
	Priority     string    `json:"priority"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// FileRecord - attachment row used for detail views.
type FileRecord struct {
	IssueKey  string    `json:"issue_key"`
	IssueURL  string    `json:"issue_url"`
	FileName  string    `json:"file_name"`
	MimeType  string    `json:"mime_type,omitempty"`
	Size      int64     `json:"size,omitempty"`
	IsText    bool      `json:"is_text"`
	Source    string    `json:"source,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ErrIssueNotFound is returned by GetIssueByKey when no row matches the key.
var ErrIssueNotFound = fmt.Errorf("issue not found")

// GetIssueByKey returns the full issue record by its tracker key. Returns
// ErrIssueNotFound if the key is unknown.
func (idx *Indexer) GetIssueByKey(ctx context.Context, key string) (*IssueRecord, error) {
	if key == "" {
		return nil, ErrIssueNotFound
	}

	sql := fmt.Sprintf(
		`SELECT id, issue_key, url, summary, description, comments_text,
		        status_name, assignee_name, author_name, queue, priority,
		        created_at, updated_at
		 FROM %s
		 WHERE issue_key = '%s'
		 LIMIT 1`,
		issuesTableName, escapeSQL(key),
	)

	req := idx.client.UtilsAPI.Sql(ctx).Body(sql)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, fmt.Errorf("get issue %s: %w", key, formatSQLError(err, sql))
	}

	if resp.ArrayOfMapmapOfStringAny == nil {
		return nil, ErrIssueNotFound
	}

	for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
		dataRows, ok := queryResult["data"].([]interface{})
		if !ok {
			continue
		}
		for _, rowRaw := range dataRows {
			rowMap, ok := rowRaw.(map[string]interface{})
			if !ok {
				continue
			}
			return &IssueRecord{
				ID:           getStringFromMap(rowMap, "id"),
				Key:          getStringFromMap(rowMap, "issue_key"),
				URL:          getStringFromMap(rowMap, "url"),
				Summary:      getStringFromMap(rowMap, "summary"),
				Description:  getStringFromMap(rowMap, "description"),
				CommentsText: getStringFromMap(rowMap, "comments_text"),
				StatusName:   getStringFromMap(rowMap, "status_name"),
				AssigneeName: getStringFromMap(rowMap, "assignee_name"),
				AuthorName:   getStringFromMap(rowMap, "author_name"),
				Queue:        getStringFromMap(rowMap, "queue"),
				Priority:     getStringFromMap(rowMap, "priority"),
				CreatedAt:    getTimeFromMap(rowMap, "created_at"),
				UpdatedAt:    getTimeFromMap(rowMap, "updated_at"),
			}, nil
		}
	}

	return nil, ErrIssueNotFound
}

// GetFilesByIssueKey returns attachment rows for the given issue, ordered by
// updated_at DESC. Returns an empty slice if the issue has no attachments.
func (idx *Indexer) GetFilesByIssueKey(ctx context.Context, key string) ([]FileRecord, error) {
	if key == "" {
		return nil, nil
	}

	sql := fmt.Sprintf(
		`SELECT issue_key, issue_url, file_name, mime_type, source, size, is_text, updated_at
		 FROM %s
		 WHERE issue_key = '%s'
		 ORDER BY updated_at DESC
		 LIMIT 200`,
		filesTableName, escapeSQL(key),
	)

	req := idx.client.UtilsAPI.Sql(ctx).Body(sql)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, fmt.Errorf("get files for %s: %w", key, formatSQLError(err, sql))
	}

	var out []FileRecord
	if resp.ArrayOfMapmapOfStringAny == nil {
		return out, nil
	}

	for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
		dataRows, ok := queryResult["data"].([]interface{})
		if !ok {
			continue
		}
		for _, rowRaw := range dataRows {
			rowMap, ok := rowRaw.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, FileRecord{
				IssueKey:  getStringFromMap(rowMap, "issue_key"),
				IssueURL:  getStringFromMap(rowMap, "issue_url"),
				FileName:  getStringFromMap(rowMap, "file_name"),
				MimeType:  getStringFromMap(rowMap, "mime_type"),
				Source:    getStringFromMap(rowMap, "source"),
				Size:      getInt64FromMap(rowMap, "size"),
				IsText:    getBoolFromMap(rowMap, "is_text"),
				UpdatedAt: getTimeFromMap(rowMap, "updated_at"),
			})
		}
	}

	return out, nil
}
