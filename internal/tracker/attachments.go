package tracker

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

var textExtAllowList = map[string]struct{}{
	".txt":  {},
	".md":   {},
	".csv":  {},
	".log":  {},
	".json": {},
	".xml":  {},
	".yaml": {},
	".yml":  {},
	".ini":  {},
	".conf": {},
}

var textMimeAllowList = map[string]struct{}{
	"application/json":       {},
	"application/xml":        {},
	"application/yaml":       {},
	"application/x-yaml":     {},
	"application/javascript": {},
	"text/csv":               {},
}

// IndexedFile - attachment prepared for indexing in Manticore
// The structure keeps both searchable content and metadata for non-text files.
type IndexedFile struct {
	ID             string    `json:"id"`
	AttachmentID   string    `json:"attachment_id"`
	IssueKey       string    `json:"issue_key"`
	IssueURL       string    `json:"issue_url"`
	FileURL        string    `json:"file_url"`
	FileName       string    `json:"file_name"`
	ContentText    string    `json:"content_text"`
	MetadataText   string    `json:"metadata_text"`
	Queue          string    `json:"queue"`
	StatusName     string    `json:"status_name"`
	Priority       string    `json:"priority"`
	AuthorName     string    `json:"author_name"`
	AssigneeName   string    `json:"assignee_name"`
	MimeType       string    `json:"mime_type"`
	Ext            string    `json:"ext"`
	Source         string    `json:"source"`
	Size           int64     `json:"size"`
	IsText         bool      `json:"is_text"`
	Downloaded     bool      `json:"downloaded"`
	DownloadFailed bool      `json:"download_failed"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type attachmentCandidate struct {
	attachment Attachment
	source     string
}

func (c *Client) extractIndexedFilesForIssue(ctx context.Context, issue Issue, comments []Comment) ([]IndexedFile, []error) {
	candidates := collectAttachmentCandidates(issue.Attachments, comments)
	var errs []error

	if len(candidates) == 0 {
		fallbackAttachments, err := c.FetchIssueAttachments(ctx, issue.Key)
		if err != nil {
			errs = append(errs, wrapAttachmentError(issue.Key, fmt.Errorf("fetch fallback attachments: %w", err)))
		} else {
			candidates = collectAttachmentCandidates(fallbackAttachments, comments)
		}
	}

	if len(candidates) == 0 {
		return nil, errs
	}

	issueURL := "https://tracker.yandex.ru/" + issue.Key
	files := make([]IndexedFile, 0, len(candidates))

	for _, candidate := range candidates {
		a := candidate.attachment
		createdAt := a.CreatedAt.Time
		if createdAt.IsZero() {
			createdAt = issue.UpdatedAt.Time
		}

		doc := IndexedFile{
			ID:           a.ID,
			AttachmentID: a.ID,
			IssueKey:     issue.Key,
			IssueURL:     issueURL,
			FileURL:      issueURL,
			FileName:     a.Name,
			Queue:        issue.Queue.Key,
			StatusName:   issue.Status.Display,
			Priority:     issue.Priority.Key,
			AuthorName:   issue.Author.Display,
			MimeType:     normalizeMime(a.MimeType),
			Ext:          normalizeExt(a.Name),
			Source:       candidate.source,
			Size:         a.Size,
			CreatedAt:    createdAt,
			UpdatedAt:    issue.UpdatedAt.Time,
			AssigneeName: "",
		}

		if issue.Assignee != nil {
			doc.AssigneeName = issue.Assignee.Display
		}

		doc.MetadataText = buildAttachmentMetadataText(doc)

		downloadURL := attachmentDownloadURL(a)
		if c.isTextAttachment(doc) && downloadURL != "" {
			content, _, err := c.DownloadURL(ctx, downloadURL)
			if err != nil {
				err = fmt.Errorf("download attachment %q (%s): %w", doc.FileName, downloadURL, err)
				err = wrapAttachmentError(issue.Key, err)
				errs = append(errs, err)
				doc.DownloadFailed = true
			} else {
				doc.Downloaded = true
				doc.ContentText = decodeTextBytes(content)
				doc.IsText = doc.ContentText != ""
			}
		}

		files = append(files, doc)
	}

	return files, errs
}

func collectAttachmentCandidates(issueAttachments []Attachment, comments []Comment) []attachmentCandidate {
	seen := map[string]struct{}{}
	candidates := make([]attachmentCandidate, 0, len(issueAttachments))

	for _, a := range issueAttachments {
		key := attachmentDedupeKey(a)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, attachmentCandidate{attachment: a, source: "issue"})
	}

	for _, comment := range comments {
		for _, a := range comment.Attachments {
			key := attachmentDedupeKey(a)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, attachmentCandidate{attachment: a, source: "comment"})
		}
	}

	return candidates
}

func attachmentDedupeKey(a Attachment) string {
	if a.ID != "" {
		return a.ID
	}
	return a.Name + "|" + a.Content + "|" + a.Self
}

// attachmentDownloadURL returns the API URL used at sync time to fetch the
// attachment bytes. It is intentionally NOT stored in IndexedFile.FileURL -
// IndexedFile.FileURL holds a user-facing UI link instead (see
// extractIndexedFilesForIssue), because this URL requires an OAuth token
// and is not navigable from a browser.
func attachmentDownloadURL(a Attachment) string {
	if a.Content != "" {
		return a.Content
	}
	if a.Self != "" {
		return a.Self
	}
	return ""
}

func normalizeExt(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		return ""
	}
	return ext
}

func normalizeMime(mime string) string {
	mime = strings.TrimSpace(strings.ToLower(mime))
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	return mime
}

func (c *Client) isTextAttachment(doc IndexedFile) bool {
	if doc.Size > 0 && doc.Size > c.maxTextFileSize {
		return false
	}
	if strings.HasPrefix(doc.MimeType, "text/") {
		return true
	}
	if _, ok := textMimeAllowList[doc.MimeType]; ok {
		return true
	}
	_, ok := textExtAllowList[doc.Ext]
	return ok
}

func decodeTextBytes(content []byte) string {
	if len(content) == 0 {
		return ""
	}

	if utf8.Valid(content) {
		return strings.TrimSpace(string(content))
	}

	if decoded, err := charmap.Windows1251.NewDecoder().Bytes(content); err == nil && utf8.Valid(decoded) {
		return strings.TrimSpace(string(decoded))
	}

	if decoded, err := charmap.KOI8R.NewDecoder().Bytes(content); err == nil && utf8.Valid(decoded) {
		return strings.TrimSpace(string(decoded))
	}

	log.Printf("Failed to decode attachment content, fallback to lossy UTF-8 conversion")
	return strings.TrimSpace(string(content))
}

func buildAttachmentMetadataText(file IndexedFile) string {
	parts := []string{
		"attachment",
		"source:" + file.Source,
		"name:" + file.FileName,
		"mime:" + file.MimeType,
		"ext:" + file.Ext,
		fmt.Sprintf("size:%d", file.Size),
		"issue:" + file.IssueKey,
	}
	return strings.Join(parts, " ")
}

func wrapAttachmentError(issueKey string, err error) error {
	return fmt.Errorf("issue %s: %w", issueKey, err)
}
