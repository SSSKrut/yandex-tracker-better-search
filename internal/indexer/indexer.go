package indexer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/SSSKrut/yandex-tracker-better-search/internal/tracker"

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

	// fullTextWildcardChars - Manticore wildcards; escaping does not defuse them.
	fullTextWildcardChars = `\?%`

	// HighlightOpen/HighlightClose - match boundaries for HIGHLIGHT(). Not HTML:
	// the result mixes markers with issue text, which must never reach a browser
	// raw. The view layer escapes the whole string, then swaps markers for tags.
	// Control characters on purpose - escapeSQL strips them while indexing, so
	// content cannot forge a marker.
	HighlightOpen  = "\x02"
	HighlightClose = "\x03"
)

var issueKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-\d+$`)

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

	// Exact - matched by link or issue key; these sort first.
	Exact bool `json:"-"`
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

// escapeQuery - escapes special characters in the search query.
//
// Backslashes are doubled on purpose: the SQL string parser eats one level, and
// a bare operator reaching the full-text parser is a "P08: syntax error".
func escapeQuery(query string, keepWildcards bool) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(query) * 2)

	for _, r := range query {
		switch {
		case r == '\'':
			// Not a full-text operator, so escape it for SQL only.
			b.WriteString(`\'`)
		case r == '*':
			if keepWildcards {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		case strings.ContainsRune(fullTextWildcardChars, r):
			// An escaped wildcard stays inside the token and never matches.
			b.WriteByte(' ')
		case isFullTextPunct(r):
			b.WriteString(`\\`)
			b.WriteRune(r)
		case unicode.IsControl(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}

	return strings.Join(strings.Fields(b.String()), " ")
}

// isFullTextPunct - ASCII punctuation for the full-text parser. It treats an
// escaped character as a token separator, so over-escaping is safe.
func isFullTextPunct(r rune) bool {
	if r > unicode.MaxASCII || !unicode.IsPrint(r) {
		return false
	}
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r)
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
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Exact != results[j].Exact {
			return results[i].Exact
		}
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (idx *Indexer) searchIssuesWithFilters(ctx context.Context, query string, filters SearchFilters, limit int) ([]SearchResult, error) {
	columns := fmt.Sprintf(`id, issue_key, url, summary, status_name, assignee_name, queue, priority, updated_at,
        HIGHLIGHT({before_match='%s', after_match='%s'}, 'summary,description,comments_text') as highlight`,
		HighlightOpen, HighlightClose)

	results, err := idx.searchTable(ctx, issuesTableName, columns, buildWhereClauses(query, filters, "url"), limit, extractIssueRow)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	return results, nil
}

func (idx *Indexer) searchFilesWithFilters(ctx context.Context, query string, filters SearchFilters, limit int) ([]SearchResult, error) {
	columns := fmt.Sprintf(`id, issue_key, issue_url, file_url, file_name, status_name, assignee_name, queue, priority,
        mime_type, source, size, is_text, updated_at,
        HIGHLIGHT({before_match='%s', after_match='%s'}, 'file_name,content_text,metadata_text') as highlight`,
		HighlightOpen, HighlightClose)

	results, err := idx.searchTable(ctx, filesTableName, columns, buildWhereClauses(query, filters, "file_url"), limit, extractFileRow)
	if err != nil {
		return nil, fmt.Errorf("search files: %w", err)
	}
	return results, nil
}

// searchTable – executes the WHERE clauses one by one, merging the results to remove duplicates
func (idx *Indexer) searchTable(
	ctx context.Context,
	table, columns string,
	clauses []whereClause,
	limit int,
	extract func(map[string]interface{}) SearchResult,
) ([]SearchResult, error) {
	var results []SearchResult
	seen := map[string]int{}

	for _, clause := range clauses {
		searchSQL := fmt.Sprintf(
			`SELECT %s
 FROM %s
 %s
 ORDER BY updated_at DESC
 LIMIT %d`,
			columns, table, clause.sql, limit)

		rows, err := idx.fetchSearchRows(ctx, searchSQL, extract)
		if err != nil {
			return nil, err
		}

		for _, row := range rows {
			key := row.Kind + "|" + row.ID
			if pos, ok := seen[key]; ok {
				if clause.exact {
					results[pos].Exact = true
				}
				continue
			}
			seen[key] = len(results)
			row.Exact = clause.exact
			results = append(results, row)
		}
	}

	return results, nil
}

func (idx *Indexer) fetchSearchRows(ctx context.Context, searchSQL string, extract func(map[string]interface{}) SearchResult) ([]SearchResult, error) {
	req := idx.client.UtilsAPI.Sql(ctx).Body(searchSQL)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, formatSQLError(err, searchSQL)
	}

	var results []SearchResult
	if resp.ArrayOfMapmapOfStringAny != nil {
		for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
			if dataRows, ok := queryResult["data"].([]interface{}); ok {
				for _, rowRaw := range dataRows {
					if rowMap, ok := rowRaw.(map[string]interface{}); ok {
						results = append(results, extract(rowMap))
					}
				}
			}
		}
	}

	return results, nil
}

type whereClause struct {
	sql   string
	exact bool
}

// buildWhereClauses – a full-text version of WHERE and, for a reference or task key,
// an attribute-based version. These cannot be combined into a single SQL statement: Manticore does not support either an OR operator between
// MATCH() and an attribute filter, or LIKE on string attributes.
func buildWhereClauses(query string, filters SearchFilters, urlField string) []whereClause {
	filterConditions := buildFilterConditions(filters)
	queryCondition := buildQueryCondition(query)
	attributeCondition := buildAttributeCondition(query, urlField)

	if queryCondition == "" && attributeCondition == "" {
		if strings.TrimSpace(query) != "" {
			return nil
		}
		return []whereClause{{sql: joinConditions("", filterConditions)}}
	}

	clauses := make([]whereClause, 0, 2)
	if queryCondition != "" {
		clauses = append(clauses, whereClause{sql: joinConditions(queryCondition, filterConditions)})
	}
	if attributeCondition != "" {
		clauses = append(clauses, whereClause{sql: joinConditions(attributeCondition, filterConditions), exact: true})
	}

	return clauses
}

func joinConditions(primary string, filterConditions []string) string {
	conditions := make([]string, 0, len(filterConditions)+1)
	if primary != "" {
		conditions = append(conditions, primary)
	}
	conditions = append(conditions, filterConditions...)

	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

func buildFilterConditions(filters SearchFilters) []string {
	var conditions []string

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

	return conditions
}

func buildQueryCondition(rawQuery string) string {
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		return ""
	}

	matchVariants := buildMatchVariants(query)
	escapedVariants := make([]string, 0, len(matchVariants))
	seen := map[string]struct{}{}

	for _, variant := range matchVariants {
		escaped := escapeQuery(variant, strings.Contains(variant, "*"))
		if !hasSearchableContent(escaped) {
			continue
		}
		if _, ok := seen[escaped]; ok {
			continue
		}
		seen[escaped] = struct{}{}
		// Parentheses are required: `|` binds tighter than the implicit AND, so
		// `a b | a* b*` parses as `a (b | a*) b*`.
		escapedVariants = append(escapedVariants, "("+escaped+")")
	}

	if len(escapedVariants) == 0 {
		return ""
	}

	return fmt.Sprintf("MATCH('%s')", strings.Join(escapedVariants, " | "))
}

func hasSearchableContent(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// buildAttributeCondition - matches a link or an issue key. url and issue_key are
// string attributes, not full-text fields, so MATCH cannot see them.
func buildAttributeCondition(rawQuery, urlField string) string {
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		return ""
	}

	var conditions []string

	if key := extractIssueKey(query); key != "" {
		conditions = append(conditions, fmt.Sprintf("issue_key = '%s'", escapeSQL(key)))
	}

	if target := urlRegexTarget(query); target != "" {
		// Anchored at the end so a link to NOVA-42 doesn't also match NOVA-429.
		pattern := escapeSQL(regexp.QuoteMeta(target) + "$")
		conditions = append(conditions, fmt.Sprintf("REGEX(%s, '%s')", urlField, pattern))
	}

	switch len(conditions) {
	case 0:
		return ""
	case 1:
		return conditions[0]
	default:
		return "(" + strings.Join(conditions, " OR ") + ")"
	}
}

// extractIssueKey - the issue key from the query ("NOVA-42") or from a link
// segment, including a link with no scheme ("tracker.yandex.ru/NOVA-42").
func extractIssueKey(query string) string {
	query = strings.TrimSpace(query)
	if query == "" || len(strings.Fields(query)) != 1 {
		return ""
	}

	if issueKeyPattern.MatchString(query) {
		return strings.ToUpper(query)
	}

	segments := strings.FieldsFunc(query, func(r rune) bool {
		return r == '/' || r == '?' || r == '#'
	})
	for i := len(segments) - 1; i >= 0; i-- {
		if issueKeyPattern.MatchString(segments[i]) {
			return strings.ToUpper(segments[i])
		}
	}

	return ""
}

// urlRegexTarget - the link without fragment or trailing slash; a stored url has neither.
func urlRegexTarget(query string) string {
	query = strings.TrimSpace(query)
	if !looksLikeURL(query) || len(strings.Fields(query)) != 1 {
		return ""
	}

	if hash := strings.IndexByte(query, '#'); hash >= 0 {
		query = query[:hash]
	}

	return strings.TrimRight(query, "/")
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
