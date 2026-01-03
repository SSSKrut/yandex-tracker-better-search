package indexer

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"ytbs/tracker"

	Manticoresearch "github.com/manticoresoftware/manticoresearch-go"
)

const tableName = "issues"

// Indexer - index for Manticoresearch
type Indexer struct {
	client *Manticoresearch.APIClient
}

// NewIndexer - creates a new Indexer instance
func NewIndexer(manticoreURL string) *Indexer {
	config := Manticoresearch.NewConfiguration()
	config.Servers[0].URL = manticoreURL

	return &Indexer{
		client: Manticoresearch.NewAPIClient(config),
	}
}

// CreateTable - creates the issues table if it doesn't exist
func (idx *Indexer) CreateTable(ctx context.Context) error {
	// Manticore CREATE TABLE syntax
	// TEXT - full-text search
	// STRING - exact match, filtering
	// BIGINT - numbers
	// TIMESTAMP - dates
	// MULTI - arrays for MVA (multi-value attributes)
	createSQL := `CREATE TABLE IF NOT EXISTS ` + tableName + ` (
		id BIGINT,
		issue_key STRING,
		url STRING,
		summary TEXT,
		description TEXT,
		comments_text TEXT,
		queue STRING,
		status STRING,
		status_name STRING,
		priority STRING,
		type STRING,
		resolution STRING,
		author STRING,
		author_name STRING,
		assignee STRING,
		assignee_name STRING,
		tags MULTI,
		created_at TIMESTAMP,
		updated_at TIMESTAMP
	) morphology='stem_en, stem_ru' html_strip='1'`

	req := idx.client.UtilsAPI.Sql(ctx).Body(createSQL)
	_, _, err := req.Execute()
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	log.Printf("Table '%s' created/verified", tableName)
	return nil
}

// IndexIssues - indexes a batch of issues
func (idx *Indexer) IndexIssues(ctx context.Context, issues []tracker.IndexedIssue) error {
	if len(issues) == 0 {
		return nil
	}

	log.Printf("Indexing %d issues...", len(issues))

	batchSize := 100
	for i := 0; i < len(issues); i += batchSize {
		end := i + batchSize
		if end > len(issues) {
			end = len(issues)
		}

		batch := issues[i:end]
		if err := idx.indexBatch(ctx, batch); err != nil {
			return fmt.Errorf("index batch %d-%d: %w", i, end, err)
		}

		log.Printf("Indexed %d/%d issues", end, len(issues))
	}

	return nil
}

// indexBatch - indexes a batch of issues
func (idx *Indexer) indexBatch(ctx context.Context, issues []tracker.IndexedIssue) error {
	for _, issue := range issues {
		// Manticore requires numeric IDs
		id, err := strconv.ParseInt(issue.ID, 10, 64)
		if err != nil {
			// fallback: hash the issue key to get a numeric ID
			id = hashString(issue.Key)
		}

		// TODO: why api fails as 409 and only SQL way works?
		sql := fmt.Sprintf(`REPLACE INTO %s (id, issue_key, url, summary, description, comments_text, 
			queue, status, status_name, priority, type, resolution, 
			author, author_name, assignee, assignee_name, created_at, updated_at) 
			VALUES (%d, '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', %d, %d)`,
			tableName,
			id,
			escapeSQL(issue.Key),
			escapeSQL(issue.URL),
			escapeSQL(issue.Summary),
			escapeSQL(issue.Description),
			escapeSQL(issue.CommentsText),
			escapeSQL(issue.Queue),
			escapeSQL(issue.Status),
			escapeSQL(issue.StatusName),
			escapeSQL(issue.Priority),
			escapeSQL(issue.Type),
			escapeSQL(issue.Resolution),
			escapeSQL(issue.Author),
			escapeSQL(issue.AuthorName),
			escapeSQL(issue.Assignee),
			escapeSQL(issue.AssigneeName),
			issue.CreatedAt.Unix(),
			issue.UpdatedAt.Unix(),
		)

		req := idx.client.UtilsAPI.Sql(ctx).Body(sql)
		_, _, err = req.Execute()
		if err != nil {
			return fmt.Errorf("replace document %s: %w", issue.Key, err)
		}
	}

	return nil
}

// Search - performs a full-text search query
func (idx *Indexer) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	// escape special characters in the query
	escapedQuery := escapeQuery(query)

	searchSQL := fmt.Sprintf(
		`SELECT id, issue_key, url, summary, status_name, assignee_name, 
		        HIGHLIGHT({before_match='<b>', after_match='</b>'}, 'summary,description,comments_text') as highlight
		 FROM %s 
		 WHERE MATCH('%s')
		 LIMIT %d
		 OPTION ranker=proximity_bm25`,
		tableName, escapedQuery, limit)

	req := idx.client.UtilsAPI.Sql(ctx).Body(searchSQL)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	var results []SearchResult

	// parse results - try SqlObjResponse first (for SELECT queries)
	if resp.ArrayOfMapmapOfStringAny != nil {

		for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {

			if dataRows, ok := queryResult["data"].([]interface{}); ok {
				for _, rowRaw := range dataRows {
					if rowMap, ok := rowRaw.(map[string]interface{}); ok {
						results = append(results, extractRow(rowMap))
					}
				}
			} else {
				log.Printf("Warning: 'data' field not found in response part: %+v", queryResult)
			}

		}
	} else {
		log.Printf("Unknown response format: %+v", resp)
	}

	return results, nil
}

// extractRow - extracts SearchResult from a map
func extractRow(row map[string]interface{}) SearchResult {
	return SearchResult{
		ID:           getStringFromMap(row, "id"),
		Key:          getStringFromMap(row, "issue_key"),
		URL:          getStringFromMap(row, "url"),
		Summary:      getStringFromMap(row, "summary"),
		StatusName:   getStringFromMap(row, "status_name"),
		AssigneeName: getStringFromMap(row, "assignee_name"),
		Highlight:    getStringFromMap(row, "highlight"),
	}
}

// SearchResult - search result
type SearchResult struct {
	ID           string `json:"id"`
	Key          string `json:"key"`
	URL          string `json:"url"`
	Summary      string `json:"summary"`
	StatusName   string `json:"status_name"`
	AssigneeName string `json:"assignee_name"`
	Highlight    string `json:"highlight"`
}

// hashString - hashes a string to an int64
func hashString(s string) int64 {
	var h int64 = 0
	for _, c := range s {
		h = 31*h + int64(c)
	}
	if h < 0 {
		h = -h
	}
	return h
}

// hashTags - converts tags to numeric IDs for MVA
func hashTags(tags []string) []int64 {
	result := make([]int64, len(tags))
	for i, tag := range tags {
		result[i] = hashString(tag)
	}
	return result
}

// escapeSQL - escapes string for SQL queries
func escapeSQL(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}

// escapeQuery - escapes special characters in the search query
func escapeQuery(query string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		"\"", "\\\"",
		"@", "\\@",
		"!", "\\!",
		"^", "\\^",
		"(", "\\(",
		")", "\\)",
		"-", "\\-",
	)
	return replacer.Replace(query)
}

// getStringFromMap - safely gets a string value from a map
func getStringFromMap(m map[string]interface{}, key string) string {
	val, ok := m[key]
	if !ok || val == nil {
		return ""
	}

	switch v := val.(type) {
	case string:
		return v
	case float64:
		// for scientific notation of float (e+18)
		return fmt.Sprintf("%.0f", v)
	case int, int64, int32:
		return fmt.Sprintf("%d", v)
	case map[string]interface{}:
		// somerimes Manticore returns nested map for some fields
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
