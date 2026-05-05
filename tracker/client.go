package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	baseURL                = "https://api.tracker.yandex.net/v3"
	maxPerPage             = 100
	defaultMaxTextFileSize = int64(2 * 1024 * 1024)
)

// Client - client for Yandex Tracker API
type Client struct {
	httpClient      *http.Client
	token           string
	orgID           string
	maxTextFileSize int64
}

// NewClient - creates a new Tracker API client
func NewClient(token, orgID string) *Client {
	maxTextFileSize := defaultMaxTextFileSize
	if raw := os.Getenv("ATTACHMENT_TEXT_MAX_BYTES"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			maxTextFileSize = parsed
		}
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		token:           token,
		orgID:           orgID,
		maxTextFileSize: maxTextFileSize,
	}
}

// doRequest - performs an HTTP request to the Tracker API
func (c *Client) doRequest(ctx context.Context, method, path string, body any) ([]byte, http.Header, error) {
	return c.doRequestURL(ctx, method, baseURL+path, body)
}

func (c *Client) doRequestURL(ctx context.Context, method, url string, body any) ([]byte, http.Header, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "OAuth "+c.token)
	req.Header.Set("X-Cloud-Org-ID", c.orgID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, resp.Header, nil
}

// DownloadURL downloads resource from Tracker API preserving auth headers.
func (c *Client) DownloadURL(ctx context.Context, url string) ([]byte, http.Header, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = baseURL + url
	}

	return c.doRequestURL(ctx, http.MethodGet, url, nil)
}
