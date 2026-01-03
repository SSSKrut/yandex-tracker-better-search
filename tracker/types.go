package tracker

import (
	"strings"
	"time"
)

// TrackerTime - time from Tracker's API (format 2025-12-19T02:02:43.196+0000)
type TrackerTime struct {
	time.Time
}

// UnmarshalJSON - custom JSON unmarshal for TrackerTime
func (t *TrackerTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		return nil
	}

	// Tracker returns us a +0000, but Go can parse only +00:00
	// So we try several formats
	formats := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z0700",
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
		time.RFC3339Nano,
	}

	var parseErr error
	for _, format := range formats {
		parsed, err := time.Parse(format, s)
		if err == nil {
			t.Time = parsed
			return nil
		}
		parseErr = err
	}

	return parseErr
}

// Issue - task(issue) in Yandex Tracker
type Issue struct {
	ID          string         `json:"id"`
	Key         string         `json:"key"`
	Summary     string         `json:"summary"`
	Description string         `json:"description"`
	Queue       QueueRef       `json:"queue"`
	Status      StatusRef      `json:"status"`
	Priority    PriorityRef    `json:"priority"`
	Type        TypeRef        `json:"type"`
	Resolution  *ResolutionRef `json:"resolution"`
	Author      UserRef        `json:"createdBy"`
	Assignee    *UserRef       `json:"assignee"`
	Followers   []UserRef      `json:"followers"`
	Tags        []string       `json:"tags"`
	CreatedAt   TrackerTime    `json:"createdAt"`
	UpdatedAt   TrackerTime    `json:"updatedAt"`
	ResolvedAt  *TrackerTime   `json:"resolvedAt"`
}

// Comment - comment on an issue
type Comment struct {
	ID        int64       `json:"id"`
	Text      string      `json:"text"`
	Author    UserRef     `json:"createdBy"`
	CreatedAt TrackerTime `json:"createdAt"`
	UpdatedAt TrackerTime `json:"updatedAt"`
}

// UserRef - user reference
type UserRef struct {
	ID      string `json:"id"`
	Display string `json:"display"`
	Login   string `json:"login,omitempty"`
}

// QueueRef - queue reference
type QueueRef struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Display string `json:"display"`
}

// StatusRef - status reference
type StatusRef struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Display string `json:"display"`
}

// PriorityRef - priority reference
type PriorityRef struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Display string `json:"display"`
}

// TypeRef - type reference
type TypeRef struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Display string `json:"display"`
}

// ResolutionRef - resolution reference
type ResolutionRef struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Display string `json:"display"`
}

// SearchRequest - request for searching issues
type SearchRequest struct {
	Query  string            `json:"query,omitempty"`
	Filter map[string]string `json:"filter,omitempty"`
	Order  string            `json:"order,omitempty"`
}
