package tracker

import (
	"testing"
	"time"
)

func TestStripHTML(t *testing.T) {
	cases := map[string]string{
		"<b>bold</b>":                "bold",
		"a&nbsp;b &amp; c":           "a b & c",
		" <p> hi </p> ":              "hi",
		"<a href='#'>link</a>&quot;": "link\"",
	}

	for in, want := range cases {
		if got := stripHTML(in); got != want {
			t.Fatalf("stripHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConvertToIndexed(t *testing.T) {
	issue := Issue{
		ID:          "123",
		Key:         "PRJ-1",
		Summary:     "Test",
		Description: "<p>hello &amp; world</p>",
		Queue:       QueueRef{Key: "PRJ"},
		Status:      StatusRef{Key: "open", Display: "Open"},
		Priority:    PriorityRef{Key: "P1"},
		Type:        TypeRef{Key: "task"},
		Author:      UserRef{ID: "u1", Display: "Alice"},
		Assignee:    &UserRef{ID: "u2", Display: "Bob"},
		Tags:        []string{"tag1", "tag2"},
		CreatedAt:   TrackerTime{time.Date(2025, 12, 19, 2, 2, 43, 0, time.UTC)},
		UpdatedAt:   TrackerTime{time.Date(2025, 12, 20, 3, 0, 0, 0, time.UTC)},
	}

	comments := []Comment{
		{ID: 1, Text: "<p>c1</p>", Author: UserRef{ID: "u3"}, CreatedAt: TrackerTime{time.Now()}},
		{ID: 2, Text: "", Author: UserRef{ID: "u4"}, CreatedAt: TrackerTime{time.Now()}},
		{ID: 3, Text: "<div>c3 &amp; more</div>", Author: UserRef{ID: "u5"}, CreatedAt: TrackerTime{time.Now()}},
	}

	idx := convertToIndexed(issue, comments)

	if idx.ID != issue.ID {
		t.Fatalf("ID mismatch: got %s", idx.ID)
	}
	if idx.URL != "https://tracker.yandex.ru/PRJ-1" {
		t.Fatalf("URL mismatch: %s", idx.URL)
	}
	if idx.Description != "hello & world" {
		t.Fatalf("Description mismatch: %q", idx.Description)
	}

	// CommentsText should include only non-empty stripped comments, joined by empty line
	wantComments := "c1\n\nc3 & more"
	if idx.CommentsText != wantComments {
		t.Fatalf("CommentsText mismatch: got %q want %q", idx.CommentsText, wantComments)
	}

	if idx.Assignee != "u2" || idx.AssigneeName != "Bob" {
		t.Fatalf("Assignee fields mismatch: %v %v", idx.Assignee, idx.AssigneeName)
	}
}
