package indexer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"ytbs/tracker"

	Manticoresearch "github.com/manticoresoftware/manticoresearch-go"
)

const (
	issuesTableName    = "issues"
	filesTableName     = "files"
	minPrefixTokenLen  = 2
	minPrefixIndexSize = 2
	minInfixTokenLen   = 2
	minInfixIndexSize  = 2
	issuesInfixFields  = "summary,description,comments_text"
	filesInfixFields   = "file_name,content_text,metadata_text"
)

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

// CreateTable - creates all required tables if they don't exist
func (idx *Indexer) CreateTable(ctx context.Context) error {
	issuesSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
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
) morphology='stem_en, stem_ru' html_strip='1' min_prefix_len='%d' min_infix_len='%d' infix_fields='%s'`,
		issuesTableName,
		minPrefixIndexSize,
		minInfixIndexSize,
		issuesInfixFields,
	)

	filesSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id BIGINT,
issue_key STRING,
issue_url STRING,
file_url STRING,
file_name TEXT,
content_text TEXT,
metadata_text TEXT,
queue STRING,
status_name STRING,
priority STRING,
author_name STRING,
assignee_name STRING,
mime_type STRING,
ext STRING,
source STRING,
size BIGINT,
is_text BOOL,
downloaded BOOL,
download_failed BOOL,
created_at TIMESTAMP,
updated_at TIMESTAMP
) morphology='stem_en, stem_ru' html_strip='1' min_prefix_len='%d' min_infix_len='%d' infix_fields='%s'`,
		filesTableName,
		minPrefixIndexSize,
		minInfixIndexSize,
		filesInfixFields,
	)

	if err := idx.execSQL(ctx, issuesSQL); err != nil {
		return fmt.Errorf("create issues table: %w", err)
	}
	if err := idx.execSQL(ctx, filesSQL); err != nil {
		return fmt.Errorf("create files table: %w", err)
	}

	log.Printf("Tables '%s' and '%s' created/verified", issuesTableName, filesTableName)
	return nil
}

func (idx *Indexer) execSQL(ctx context.Context, sql string) error {
	req := idx.client.UtilsAPI.Sql(ctx).Body(sql)
	_, _, err := req.Execute()
	if err == nil {
		return nil
	}
	return formatSQLError(err, sql)
}

func formatSQLError(err error, sql string) error {
	type bodyError interface {
		Body() []byte
	}
	var be bodyError
	if errors.As(err, &be) {
		body := strings.TrimSpace(string(be.Body()))
		if body != "" {
			return fmt.Errorf("sql error: %s (sql: %s)", body, sql)
		}
	}

	return fmt.Errorf("sql error: %w (sql: %s)", err, sql)
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
		if err := idx.indexIssuesBatch(ctx, batch); err != nil {
			return fmt.Errorf("index batch %d-%d: %w", i, end, err)
		}

		log.Printf("Indexed %d/%d issues", end, len(issues))
	}

	return nil
}

func (idx *Indexer) indexIssuesBatch(ctx context.Context, issues []tracker.IndexedIssue) error {
	for _, issue := range issues {
		id, err := strconv.ParseInt(issue.ID, 10, 64)
		if err != nil {
			id = hashString(issue.Key)
		}

		sql := fmt.Sprintf(`REPLACE INTO %s (id, issue_key, url, summary, description, comments_text,
queue, status, status_name, priority, type, resolution,
author, author_name, assignee, assignee_name, created_at, updated_at)
VALUES (%d, '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', %d, %d)`,
			issuesTableName,
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

		if err := idx.execSQL(ctx, sql); err != nil {
			return fmt.Errorf("replace issue %s: %w", issue.Key, err)
		}
	}

	return nil
}

// IndexFiles - indexes a batch of files
func (idx *Indexer) IndexFiles(ctx context.Context, files []tracker.IndexedFile) error {
	if len(files) == 0 {
		return nil
	}

	log.Printf("Indexing %d files...", len(files))

	batchSize := 100
	for i := 0; i < len(files); i += batchSize {
		end := i + batchSize
		if end > len(files) {
			end = len(files)
		}

		batch := files[i:end]
		if err := idx.indexFilesBatch(ctx, batch); err != nil {
			return fmt.Errorf("index files batch %d-%d: %w", i, end, err)
		}

		log.Printf("Indexed %d/%d files", end, len(files))
	}

	return nil
}

func (idx *Indexer) indexFilesBatch(ctx context.Context, files []tracker.IndexedFile) error {
	for _, file := range files {
		id, err := strconv.ParseInt(file.ID, 10, 64)
		if err != nil {
			id = hashString(file.IssueKey + "|" + file.FileName + "|" + file.AttachmentID)
		}

		sql := fmt.Sprintf(`REPLACE INTO %s (id, issue_key, issue_url, file_url, file_name, content_text, metadata_text,
queue, status_name, priority, author_name, assignee_name, mime_type, ext, source, size,
is_text, downloaded, download_failed, created_at, updated_at)
VALUES (%d, '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', %d, %d, %d, %d, %d, %d)`,
			filesTableName,
			id,
			escapeSQL(file.IssueKey),
			escapeSQL(file.IssueURL),
			escapeSQL(file.FileURL),
			escapeSQL(file.FileName),
			escapeSQL(file.ContentText),
			escapeSQL(file.MetadataText),
			escapeSQL(file.Queue),
			escapeSQL(file.StatusName),
			escapeSQL(file.Priority),
			escapeSQL(file.AuthorName),
			escapeSQL(file.AssigneeName),
			escapeSQL(file.MimeType),
			escapeSQL(file.Ext),
			escapeSQL(file.Source),
			file.Size,
			boolToInt(file.IsText),
			boolToInt(file.Downloaded),
			boolToInt(file.DownloadFailed),
			file.CreatedAt.Unix(),
			file.UpdatedAt.Unix(),
		)

		if err := idx.execSQL(ctx, sql); err != nil {
			return fmt.Errorf("replace file %s: %w", file.FileName, err)
		}
	}

	return nil
}

// Search - performs a full-text search query
func (idx *Indexer) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return idx.SearchWithFilters(ctx, query, SearchFilters{}, limit)
}

// SearchResult - search result
type SearchResult struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Key            string    `json:"key"`
	URL            string    `json:"url"`
	Summary        string    `json:"summary"`
	StatusName     string    `json:"status_name"`
	AssigneeName   string    `json:"assignee_name"`
	Queue          string    `json:"queue"`
	Priority       string    `json:"priority"`
	Highlight      string    `json:"highlight"`
	FileName       string    `json:"file_name"`
	MimeType       string    `json:"mime_type"`
	Source         string    `json:"source"`
	Size           int64     `json:"size"`
	IsTextFile     bool      `json:"is_text_file"`
	ParentIssueURL string    `json:"parent_issue_url"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func extractIssueRow(row map[string]interface{}) SearchResult {
	return SearchResult{
		ID:           getStringFromMap(row, "id"),
		Kind:         "issue",
		Key:          getStringFromMap(row, "issue_key"),
		URL:          getStringFromMap(row, "url"),
		Summary:      getStringFromMap(row, "summary"),
		StatusName:   getStringFromMap(row, "status_name"),
		AssigneeName: getStringFromMap(row, "assignee_name"),
		Queue:        getStringFromMap(row, "queue"),
		Priority:     getStringFromMap(row, "priority"),
		UpdatedAt:    getTimeFromMap(row, "updated_at"),
		Highlight:    getStringFromMap(row, "highlight"),
	}
}

func extractFileRow(row map[string]interface{}) SearchResult {
	return SearchResult{
		ID:             getStringFromMap(row, "id"),
		Kind:           "file",
		Key:            getStringFromMap(row, "issue_key"),
		URL:            getStringFromMap(row, "file_url"),
		Summary:        getStringFromMap(row, "file_name"),
		StatusName:     getStringFromMap(row, "status_name"),
		AssigneeName:   getStringFromMap(row, "assignee_name"),
		Queue:          getStringFromMap(row, "queue"),
		Priority:       getStringFromMap(row, "priority"),
		Highlight:      getStringFromMap(row, "highlight"),
		FileName:       getStringFromMap(row, "file_name"),
		MimeType:       getStringFromMap(row, "mime_type"),
		Source:         getStringFromMap(row, "source"),
		Size:           getInt64FromMap(row, "size"),
		IsTextFile:     getBoolFromMap(row, "is_text"),
		ParentIssueURL: getStringFromMap(row, "issue_url"),
		UpdatedAt:      getTimeFromMap(row, "updated_at"),
	}
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

// escapeSQL - escapes string for SQL queries
func escapeSQL(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '\'':
			b.WriteString("\\'")
		case '\n':
			b.WriteString("\\n")
		case '\r', '\t':
			b.WriteByte(' ')
		case 0:
			// Drop NUL bytes to avoid SQL parser issues.
		default:
			if unicode.IsControl(r) {
				b.WriteByte(' ')
			} else {
				b.WriteRune(r)
			}
		}
	}

	return strings.TrimSpace(b.String())
}

// escapeQuery - escapes special characters in the search query
func escapeQuery(query string, keepWildcards bool) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	replacements := []string{
		"\\", "\\\\",
		"'", "\\'",
		"\"", "\\\"",
		":", "\\:",
		"@", "\\@",
		"!", "\\!",
		"^", "\\^",
		"~", "\\~",
		"/", "\\/",
		"[", "\\[",
		"]", "\\]",
		"{", "\\{",
		"}", "\\}",
		"|", "\\|",
		"&", "\\&",
		"=", "\\=",
		"<", "\\<",
		">", "\\>",
		"?", "\\?",
		"(", "\\(",
		")", "\\)",
		"-", "\\-",
	}
	if !keepWildcards {
		replacements = append(replacements, "*", "\\*")
	}

	replacer := strings.NewReplacer(replacements...)
	escaped := replacer.Replace(query)
	return strings.Join(strings.Fields(escaped), " ")
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
		return fmt.Sprintf("%.0f", v)
	case int, int64, int32:
		return fmt.Sprintf("%d", v)
	case map[string]interface{}:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func getInt64FromMap(m map[string]interface{}, key string) int64 {
	val, ok := m[key]
	if !ok || val == nil {
		return 0
	}

	switch v := val.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return parsed
		}
	}

	return 0
}

func getBoolFromMap(m map[string]interface{}, key string) bool {
	val, ok := m[key]
	if !ok || val == nil {
		return false
	}

	switch v := val.(type) {
	case bool:
		return v
	case int, int64, int32:
		return fmt.Sprintf("%v", v) != "0"
	case float64:
		return v != 0
	case string:
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "1" || v == "true" || v == "yes"
	default:
		return false
	}
}

func getTimeFromMap(m map[string]interface{}, key string) time.Time {
	unix := getInt64FromMap(m, key)
	if unix <= 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// FilterOptions - available filter values
type FilterOptions struct {
	Queues     []string `json:"queues"`
	Statuses   []string `json:"statuses"`
	Priorities []string `json:"priorities"`
	Authors    []string `json:"authors"`
	Assignees  []string `json:"assignees"`
}

// GetFilterOptions - returns unique values for filters
func (idx *Indexer) GetFilterOptions(ctx context.Context) (*FilterOptions, error) {
	options := &FilterOptions{}

	getDistinct := func(field string) ([]string, error) {
		sql := fmt.Sprintf(`SELECT %s, COUNT(*) as cnt FROM %s GROUP BY %s ORDER BY cnt DESC LIMIT 100`, field, issuesTableName, field)
		req := idx.client.UtilsAPI.Sql(ctx).Body(sql)
		resp, _, err := req.Execute()
		if err != nil {
			return nil, err
		}

		var values []string
		if resp.ArrayOfMapmapOfStringAny != nil {
			for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
				if dataRows, ok := queryResult["data"].([]interface{}); ok {
					for _, rowRaw := range dataRows {
						if rowMap, ok := rowRaw.(map[string]interface{}); ok {
							val := getStringFromMap(rowMap, field)
							if val != "" {
								values = append(values, val)
							}
						}
					}
				}
			}
		}
		return values, nil
	}

	var err error
	options.Queues, err = getDistinct("queue")
	if err != nil {
		log.Printf("Error getting queues: %v", err)
	}

	options.Statuses, err = getDistinct("status_name")
	if err != nil {
		log.Printf("Error getting statuses: %v", err)
	}

	options.Priorities, err = getDistinct("priority")
	if err != nil {
		log.Printf("Error getting priorities: %v", err)
	}

	options.Authors, err = getDistinct("author_name")
	if err != nil {
		log.Printf("Error getting authors: %v", err)
	}

	options.Assignees, err = getDistinct("assignee_name")
	if err != nil {
		log.Printf("Error getting assignees: %v", err)
	}

	return options, nil
}

// SearchFilters - filter parameters for search
type SearchFilters struct {
	Queue    string
	Status   string
	Priority string
	Author   string
	Assignee string
}

// SearchWithFilters - performs a full-text search query with filters
func (idx *Indexer) SearchWithFilters(ctx context.Context, query string, filters SearchFilters, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	issues, err := idx.searchIssuesWithFilters(ctx, query, filters, limit)
	if err != nil {
		return nil, err
	}

	files, err := idx.searchFilesWithFilters(ctx, query, filters, limit)
	if err != nil {
		return nil, err
	}

	results := append(issues, files...)
	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (idx *Indexer) searchIssuesWithFilters(ctx context.Context, query string, filters SearchFilters, limit int) ([]SearchResult, error) {
	whereClause := buildWhereClause(query, filters, "url")

	searchSQL := fmt.Sprintf(
		`SELECT id, issue_key, url, summary, status_name, assignee_name, queue, priority, updated_at,
        HIGHLIGHT({before_match='<b>', after_match='</b>'}, 'summary,description,comments_text') as highlight
 FROM %s
 %s
 ORDER BY updated_at DESC
 LIMIT %d`,
		issuesTableName, whereClause, limit)

	req := idx.client.UtilsAPI.Sql(ctx).Body(searchSQL)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", formatSQLError(err, searchSQL))
	}

	var results []SearchResult
	if resp.ArrayOfMapmapOfStringAny != nil {
		for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
			if dataRows, ok := queryResult["data"].([]interface{}); ok {
				for _, rowRaw := range dataRows {
					if rowMap, ok := rowRaw.(map[string]interface{}); ok {
						results = append(results, extractIssueRow(rowMap))
					}
				}
			}
		}
	}

	return results, nil
}

func (idx *Indexer) searchFilesWithFilters(ctx context.Context, query string, filters SearchFilters, limit int) ([]SearchResult, error) {
	whereClause := buildWhereClause(query, filters, "file_url")

	searchSQL := fmt.Sprintf(
		`SELECT id, issue_key, issue_url, file_url, file_name, status_name, assignee_name, queue, priority,
        mime_type, source, size, is_text, updated_at,
        HIGHLIGHT({before_match='<b>', after_match='</b>'}, 'file_name,content_text,metadata_text') as highlight
 FROM %s
 %s
 ORDER BY updated_at DESC
 LIMIT %d`,
		filesTableName, whereClause, limit)

	req := idx.client.UtilsAPI.Sql(ctx).Body(searchSQL)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, fmt.Errorf("search files: %w", formatSQLError(err, searchSQL))
	}

	var results []SearchResult
	if resp.ArrayOfMapmapOfStringAny != nil {
		for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
			if dataRows, ok := queryResult["data"].([]interface{}); ok {
				for _, rowRaw := range dataRows {
					if rowMap, ok := rowRaw.(map[string]interface{}); ok {
						results = append(results, extractFileRow(rowMap))
					}
				}
			}
		}
	}

	return results, nil
}

func buildWhereClause(query string, filters SearchFilters, urlField string) string {
	var conditions []string

	if queryCondition := buildQueryCondition(query, urlField); queryCondition != "" {
		conditions = append(conditions, queryCondition)
	}

	if filters.Queue != "" {
		conditions = append(conditions, fmt.Sprintf("queue = '%s'", escapeSQL(filters.Queue)))
	}
	if filters.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status_name = '%s'", escapeSQL(filters.Status)))
	}
	if filters.Priority != "" {
		conditions = append(conditions, fmt.Sprintf("priority = '%s'", escapeSQL(filters.Priority)))
	}
	if filters.Author != "" {
		conditions = append(conditions, fmt.Sprintf("author_name = '%s'", escapeSQL(filters.Author)))
	}
	if filters.Assignee != "" {
		conditions = append(conditions, fmt.Sprintf("assignee_name = '%s'", escapeSQL(filters.Assignee)))
	}

	if len(conditions) > 0 {
		return "WHERE " + strings.Join(conditions, " AND ")
	}
	return ""
}

func buildQueryCondition(rawQuery, urlField string) string {
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		return ""
	}

	matchVariants := buildMatchVariants(query)
	escapedVariants := make([]string, 0, len(matchVariants))
	for _, variant := range matchVariants {
		escaped := escapeQuery(variant, strings.Contains(variant, "*"))
		if escaped != "" {
			escapedVariants = append(escapedVariants, escaped)
		}
	}

	if len(escapedVariants) == 0 {
		return ""
	}

	matchExpr := strings.Join(escapedVariants, " | ")
	condition := fmt.Sprintf("MATCH('%s')", matchExpr)
	if looksLikeURL(query) {
		condition = "(" + condition + fmt.Sprintf(" OR %s LIKE '%%%s%%')", urlField, escapeSQL(query)) + ")"
	}

	return condition
}

func buildMatchVariants(query string) []string {
	variants := make([]string, 0, 3)
	seen := map[string]struct{}{}

	addVariant := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		variants = append(variants, v)
	}

	addVariant(query)
	addVariant(buildPrefixVariant(query))
	addVariant(buildInfixVariant(query))

	if looksLikeURL(query) {
		if parsed, err := url.Parse(query); err == nil {
			addVariant(parsed.Host)
			addVariant(buildPrefixVariant(parsed.Host))
			addVariant(buildInfixVariant(parsed.Host))
			normalized := normalizeURLLikeText(parsed.Host + " " + parsed.Path + " " + parsed.RawQuery + " " + parsed.Fragment)
			addVariant(normalized)
			addVariant(buildPrefixVariant(normalized))
			addVariant(buildInfixVariant(normalized))
		}
		normalized := normalizeURLLikeText(query)
		addVariant(normalized)
		addVariant(buildPrefixVariant(normalized))
		addVariant(buildInfixVariant(normalized))
	}

	return variants
}

func buildPrefixVariant(query string) string {
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return ""
	}

	changed := false
	for i, token := range tokens {
		if shouldAddPrefixWildcard(token) {
			tokens[i] = token + "*"
			changed = true
		}
	}

	if !changed {
		return ""
	}

	return strings.Join(tokens, " ")
}

func shouldAddPrefixWildcard(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || strings.HasSuffix(token, "*") {
		return false
	}
	return len([]rune(token)) >= minPrefixTokenLen
}

func buildInfixVariant(query string) string {
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return ""
	}

	changed := false
	for i, token := range tokens {
		if shouldAddInfixWildcard(token) {
			tokens[i] = "*" + token + "*"
			changed = true
		}
	}

	if !changed {
		return ""
	}

	return strings.Join(tokens, " ")
}

func shouldAddInfixWildcard(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "*") {
		return false
	}
	return len([]rune(token)) >= minInfixTokenLen
}

func normalizeURLLikeText(s string) string {
	replacer := strings.NewReplacer(
		":", " ",
		"/", " ",
		"?", " ",
		"&", " ",
		"=", " ",
		"#", " ",
		"%", " ",
		".", " ",
		"-", " ",
		"_", " ",
	)
	parts := strings.Fields(replacer.Replace(s))
	return strings.Join(parts, " ")
}

func looksLikeURL(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return strings.Contains(s, "://") || strings.HasPrefix(s, "www.")
}
