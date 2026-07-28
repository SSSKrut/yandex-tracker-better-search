package tracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_AuthHeader_OAuth(t *testing.T) {
	got := captureAuthHeader(t, NewClient("tok123", "org"))
	if want := "OAuth tok123"; got != want {
		t.Fatalf("OAuth scheme: got %q, want %q", got, want)
	}
}

func TestClient_AuthHeader_IAM(t *testing.T) {
	got := captureAuthHeader(t, NewClientWithAuth("iam456", AuthIAM, "org", 0))
	if want := "Bearer iam456"; got != want {
		t.Fatalf("IAM scheme: got %q, want %q", got, want)
	}
}

func TestClient_AuthHeader_UnknownSchemeFallsBackToOAuth(t *testing.T) {
	got := captureAuthHeader(t, NewClientWithAuth("tok", "garbage", "org", 0))
	if want := "OAuth tok"; got != want {
		t.Fatalf("fallback: got %q, want %q", got, want)
	}
}

func captureAuthHeader(t *testing.T, c *Client) string {
	t.Helper()
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Get("Authorization")
		w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)

	if _, _, err := c.doRequestURL(context.Background(), http.MethodGet, srv.URL, nil); err != nil {
		t.Fatalf("doRequestURL: %v", err)
	}
	return received
}
