package tracker

import "testing"

func TestAttachmentDownloadURL(t *testing.T) {
	cases := []struct {
		name string
		in   Attachment
		want string
	}{
		{
			name: "prefers content over self",
			in:   Attachment{Content: "https://api.tracker.yandex.net/v3/issues/PRJ-1/attachments/1/file.txt", Self: "https://api.tracker.yandex.net/v3/attachments/1"},
			want: "https://api.tracker.yandex.net/v3/issues/PRJ-1/attachments/1/file.txt",
		},
		{
			name: "falls back to self when content empty",
			in:   Attachment{Self: "https://api.tracker.yandex.net/v3/attachments/1"},
			want: "https://api.tracker.yandex.net/v3/attachments/1",
		},
		{
			name: "empty when both empty",
			in:   Attachment{},
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := attachmentDownloadURL(c.in); got != c.want {
				t.Fatalf("attachmentDownloadURL = %q, want %q", got, c.want)
			}
		})
	}
}

func TestExtractIndexedFiles_UsesIssueURLAsPublicLink(t *testing.T) {
	c := &Client{maxTextFileSize: 1 << 20}
	issue := Issue{
		Key: "PRJ-42",
		Attachments: []Attachment{
			{
				ID:       "777",
				Name:     "doc.bin",
				Content:  "https://api.tracker.yandex.net/v3/issues/PRJ-42/attachments/777/doc.bin",
				Self:     "https://api.tracker.yandex.net/v3/attachments/777",
				MimeType: "application/octet-stream",
				Size:     128,
			},
		},
	}

	files, _ := c.extractIndexedFilesForIssue(t.Context(), issue, nil)
	if len(files) != 1 {
		t.Fatalf("expected 1 indexed file, got %d", len(files))
	}
	got := files[0]

	wantURL := "https://tracker.yandex.ru/PRJ-42"
	if got.IssueURL != wantURL {
		t.Fatalf("IssueURL = %q, want %q", got.IssueURL, wantURL)
	}
	if got.FileURL != wantURL {
		t.Fatalf("FileURL must point to the issue UI page, got %q want %q", got.FileURL, wantURL)
	}
	if got.AttachmentID != "777" {
		t.Fatalf("AttachmentID = %q, want %q", got.AttachmentID, "777")
	}
}
