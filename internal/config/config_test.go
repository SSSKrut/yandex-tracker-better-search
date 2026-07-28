package config

import (
	"strings"
	"testing"
	"time"
)

// clearEnv drops every config variable so a test doesn't depend on whoever
// runs it - a developer may well have .env exported.
func clearEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"TRACKER_OAUTH_TOKEN", "TRACKER_IAM_TOKEN", "TRACKER_CLOUD_ORG_ID",
		"ATTACHMENT_TEXT_MAX_BYTES", "MANTICORE_URL",
		"SYNC_STATE_PATH", "SYNC_OVERLAP", "MAP_CACHE_TTL",
		"MAP_MAX_ISSUES", "MAP_MAX_FILES", "MAP_MAX_FILE_NAMES", "MAP_MAX_DOC_CHARS",
		"MAP_MAX_VOCAB", "MAP_MAX_NEIGHBORS", "MAP_SIM_DIMS", "MAP_CLUSTER_K",
		"MCP_AUTH_TOKEN",
	} {
		t.Setenv(name, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ManticoreURL", cfg.ManticoreURL, "http://localhost:9308"},
		{"SyncStatePath", cfg.SyncStatePath, "backups/sync_state.json"},
		{"SyncOverlap", cfg.SyncOverlap, 2 * time.Minute},
		{"MapCacheTTL", cfg.MapCacheTTL, 10 * time.Minute},
		{"AttachmentTextMaxBytes", cfg.AttachmentTextMaxBytes, int64(2 * 1024 * 1024)},
		{"MapMaxIssues", cfg.MapMaxIssues, 1000},
		{"MapSimilarityDims", cfg.MapSimilarityDims, 16},
		{"MapClusterK", cfg.MapClusterK, 0},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_ReadsEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("MANTICORE_URL", "http://manticore:9308")
	t.Setenv("MAP_CACHE_TTL", "90s")
	t.Setenv("SYNC_OVERLAP", "1h30m")
	t.Setenv("MAP_MAX_ISSUES", "42")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ManticoreURL != "http://manticore:9308" {
		t.Errorf("ManticoreURL = %q", cfg.ManticoreURL)
	}
	if cfg.MapCacheTTL != 90*time.Second {
		t.Errorf("MapCacheTTL = %v, want 90s", cfg.MapCacheTTL)
	}
	if cfg.SyncOverlap != 90*time.Minute {
		t.Errorf("SyncOverlap = %v, want 1h30m", cfg.SyncOverlap)
	}
	if cfg.MapMaxIssues != 42 {
		t.Errorf("MapMaxIssues = %d, want 42", cfg.MapMaxIssues)
	}
}

// TestLoad_RejectsBadValues - the point of the rewrite: an unparseable value
// used to become the default in silence, leaving the operator sure it applied.
func TestLoad_RejectsBadValues(t *testing.T) {
	tests := map[string]string{
		"MAP_CACHE_TTL":             "не-время",
		"SYNC_OVERLAP":              "5",
		"MAP_MAX_ISSUES":            "много",
		"ATTACHMENT_TEXT_MAX_BYTES": "2MB",
	}

	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(name, value)

			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q must be rejected, got no error", name, value)
			} else if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q should name the offending variable %q", err, name)
			}
		})
	}
}

func TestTrackerAuth_IAMWins(t *testing.T) {
	tests := []struct {
		name      string
		iam       string
		oauth     string
		wantToken string
		wantIAM   bool
	}{
		{"только oauth", "", "oauth-token", "oauth-token", false},
		{"только iam", "iam-token", "", "iam-token", true},
		{"iam выигрывает", "iam-token", "oauth-token", "iam-token", true},
		{"ничего не задано", "", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{IAMToken: tc.iam, OAuthToken: tc.oauth}

			token, isIAM := cfg.TrackerAuth()
			if token != tc.wantToken || isIAM != tc.wantIAM {
				t.Errorf("TrackerAuth() = (%q, %v), want (%q, %v)", token, isIAM, tc.wantToken, tc.wantIAM)
			}
		})
	}
}

func TestRequireTracker(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		errMatch string
	}{
		{"всё задано", Config{OAuthToken: "tok", OrgID: "org"}, ""},
		{"iam вместо oauth", Config{IAMToken: "tok", OrgID: "org"}, ""},
		{"нет токена", Config{OrgID: "org"}, "TRACKER_OAUTH_TOKEN"},
		{"нет организации", Config{OAuthToken: "tok"}, "TRACKER_CLOUD_ORG_ID"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.RequireTracker()

			if tc.errMatch == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error mentioning %s", tc.errMatch)
			}
			if !strings.Contains(err.Error(), tc.errMatch) {
				t.Errorf("error %q does not mention %q", err, tc.errMatch)
			}
		})
	}
}

// TestDescription_CoversEveryVariable keeps the --help list from drifting away
// from what is actually read.
func TestDescription_CoversEveryVariable(t *testing.T) {
	text, err := Description()
	if err != nil {
		t.Fatalf("Description: %v", err)
	}

	for _, name := range []string{
		"TRACKER_OAUTH_TOKEN", "TRACKER_IAM_TOKEN", "TRACKER_CLOUD_ORG_ID",
		"MANTICORE_URL", "ATTACHMENT_TEXT_MAX_BYTES",
		"SYNC_STATE_PATH", "SYNC_OVERLAP", "MAP_CACHE_TTL", "MCP_AUTH_TOKEN",
	} {
		if !strings.Contains(text, name) {
			t.Errorf("description does not mention %s", name)
		}
	}
}
